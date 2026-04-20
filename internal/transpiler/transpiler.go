// Package transpiler walks the TypeScript AST and emits Lua code.
package transpiler

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/microsoft/typescript-go/shim/scanner"
	"github.com/realcoldfry/tslua/internal/lua"
	"github.com/realcoldfry/tslua/internal/lualibinfo"
	"github.com/realcoldfry/tslua/internal/sourcemap"
)

// shebangRe matches a shebang line at the start of a file (LF or CRLF).
var shebangRe = regexp.MustCompile(`^#!.*\r?\n`)

// ==========================================================================
// Scope & symbol tracking types
// ==========================================================================

// ScopeType identifies the kind of scope being tracked.
type ScopeType int

const (
	ScopeFile ScopeType = 1 << iota
	ScopeFunction
	ScopeSwitch
	ScopeLoop
	ScopeConditional
	ScopeBlock
	ScopeTry
	ScopeCatch
)

// SymbolID is a unique identifier for a tracked symbol within a transpilation.
type SymbolID int

// SymbolInfo tracks when a symbol was first referenced.
type SymbolInfo struct {
	Symbol         *ast.Symbol
	FirstSeenAtPos int // source position where first referenced
}

// FunctionDefinitionInfo tracks a function declaration registered in a scope.
type FunctionDefinitionInfo struct {
	ReferencedSymbols map[SymbolID]int // symbols referenced within the function body (count)
	Definition        lua.Statement    // the transformed lua statement (local decl or assignment)
}

// TrackedVarDecl pairs a variable declaration statement with the symbol IDs of its identifiers.
type TrackedVarDecl struct {
	Stmt      *lua.VariableDeclarationStatement
	SymbolIDs []SymbolID
}

// Scope tracks symbol references and declarations within a lexical scope.
type Scope struct {
	Type              ScopeType
	ID                int
	Node              *ast.Node
	ReferencedSymbols map[SymbolID]int // symbols referenced in this scope (reference count)
	VariableDecls     []TrackedVarDecl // variable declarations for hoisting
	FunctionDefs      map[SymbolID]*FunctionDefinitionInfo
	ImportStatements  []lua.Statement // import statements to hoist to top

	// Flags set when a return/break/continue inside a try/catch needs to be
	// deferred to post-check logic after the pcall (sync) or awaiter chain
	// (async). Break/continue inside the pcall/awaiter function body can't
	// use goto/break directly because Lua forbids crossing function boundaries
	// with either, so we assign a sentinel flag and return, then dispatch
	// after the try wrapper.
	TryHasReturn         bool
	TryDeferredTransfers []DeferredTransfer
}

// TransferKind identifies the kind of deferred control-flow transfer out of a
// try/catch body.
type TransferKind int

const (
	TransferBreak TransferKind = iota
	TransferContinue
)

// DeferredTransfer describes a break/continue that had to be replaced with a
// sentinel-flag assignment + return because the jump would have crossed the
// pcall/awaiter function boundary. The transformTryStatement visitor consumes
// these to emit flag declarations and post-pcall dispatch.
type DeferredTransfer struct {
	Kind     TransferKind
	TSLabel  string // "" for unlabeled
	FlagName string // Lua identifier for the sentinel flag
	Dispatch []lua.Statement
}

// Transpiler holds the state needed for transpiling a single source file.
type Transpiler struct {
	program                   *compiler.Program
	checker                   *checker.Checker
	sourceFile                *ast.SourceFile         // current source file being transpiled
	sourceRoot                string                  // root directory for computing relative module paths
	luaTarget                 LuaTarget               // target Lua version (LuaJIT, 5.1, etc.)
	emitMode                  EmitMode                // tstl (default) or optimized
	exportAsGlobal            bool                    // strip module wrapper, emit exports as globals
	luaLibImport              LuaLibImportKind        // how lualib features are included: require, inline, none
	lualibInlineContent       string                  // pre-loaded lualib bundle content for inline mode (full bundle fallback)
	lualibFeatureData         *lualibinfo.FeatureData // per-feature data for selective inline mode
	noImplicitSelf            bool                    // unresolved calls default to no-self (for lualib code calling Lua stdlib)
	noImplicitGlobalVariables bool                    // force local declarations in script-mode top-level scope
	compilerOptions           *core.CompilerOptions   // TS compiler options from tsconfig (target, useDefineForClassFields, etc.)
	isModule                  bool                    // true if the source file has import/export statements
	isStrict                  bool                    // true when strict mode (modules with target >= ES2015, or alwaysStrict)
	lualibs                   []string                // lualib features needed (e.g. "__TS__TypeOf")
	lualibSet                 map[string]bool         // dedup set for lualibs
	tempCount                 int                     // counter for generating unique temp variable names
	exportedNames             map[string]bool         // names that have been exported (for identifier rewriting)
	namedExports              map[string][]string     // local name → export names for `export { local as exported }`
	continueLabels            []string                // stack of active continue labels for nested loops
	forLoopIncrementors       [][]lua.Statement       // stack of for-loop incrementor emit (pre-transformed, for Luau native continue)
	forLoopPreContinue        [][]lua.Statement       // stack of per-loop statements to emit before native continue (e.g. sync-back for captured+reassigned let)
	loopVarRenames            map[*ast.Symbol]string  // for-loop let symbols renamed when the outer counter is separated from the per-iteration binding
	breakLabels               map[string]string       // TS label name → Lua break label name (for labeled break)
	continueLabelMap          map[string]string       // TS label name → Lua continue label name (for labeled continue)
	labelScopeDepths          map[string]int          // TS label name → scope-stack length at label registration (target loop/block index)
	activeLabeledContinue     string                  // Lua label to emit at continue point of next loop (set by transformLabeledStatement)
	destructuredParamNames    map[*ast.Node]string    // maps binding pattern nodes to their generated temp names
	bindingPatternCount       int                     // per-function counter for ____bindingPatternN naming
	precedingStatementsStack  [][]lua.Statement       // stack for capturing expression setup statements
	asyncDepth                int                     // >0 when inside an async function body
	generatorDepth            int                     // >0 when inside a generator function body
	tryDepth                  int                     // >0 when inside a try block (return → return true, value)
	currentNamespace          string                  // name of the current namespace being transpiled (for inner exports)
	currentNamespaceNode      *ast.Node               // current ModuleDeclaration node (for building qualified namespace paths)
	currentClassRef           lua.Expression          // current class identifier for method transforms
	currentBaseClassName      string                  // parent class name for super calls (empty if no base or complex expression)
	inStaticMethod            bool                    // true when inside a static class method (super omits .prototype)
	diagnostics               []*ast.Diagnostic       // collected transpilation diagnostics
	optimizedVarArgs          map[SymbolID]bool       // rest params that can use ... directly (spread-only usage)
	validationStack           map[typePair]bool       // per-call-stack guard against infinite recursion in assignment validation
	sourceMapEnabled          bool                    // when true, Lua AST nodes get source positions for source map generation
	traceEnabled              bool                    // when true, emit --[[trace: ...]] comments on statements
	removeComments            bool                    // when true, strip all comments from output (removeComments tsconfig option)
	classStyle                ClassStyle              // alternative class emit style (default: TSTL prototype chains)
	usesMiddleclass           bool                    // at least one class emitted under ClassStyleMiddleclass (triggers require("middleclass") header)
	noResolvePaths            map[string]bool         // module specifiers to emit as-is without resolving (TSTL noResolvePaths)
	crossFileEnums            map[string]bool         // enum names declared in 2+ source files (need global scope for merging)
	dependencies              []ModuleDependency      // module dependencies discovered during transformation

	// Scope stack & symbol tracking (replaces scopeDepth + hoistedFunctionsStack)
	scopeStack    []*Scope
	lastScopeID   int
	symbolIDMap   map[*ast.Symbol]SymbolID // ts symbol → our ID
	symbolInfoMap map[SymbolID]*SymbolInfo // our ID → info
	lastSymbolID  SymbolID
}

// useDefineForClassFields returns true when field initializers use [[Define]]
// semantics (run before constructor body). Explicit tsconfig setting takes
// precedence; otherwise defaults to true for ES2022+ and false for earlier targets.
func (t *Transpiler) useDefineForClassFields() bool {
	if t.compilerOptions != nil && !t.compilerOptions.UseDefineForClassFields.IsUnknown() {
		return t.compilerOptions.UseDefineForClassFields.IsTrue()
	}
	if t.compilerOptions != nil && t.compilerOptions.Target >= core.ScriptTargetES2022 {
		return true
	}
	return false
}

// addError records an error diagnostic at the given node's location.
func (t *Transpiler) addError(node *ast.Node, code dw.DiagCode, message string) {
	t.diagnostics = append(t.diagnostics, dw.NewErrorForNode(t.sourceFile, node, code, message))
}

func (t *Transpiler) addWarning(node *ast.Node, code dw.DiagCode, message string) {
	t.diagnostics = append(t.diagnostics, dw.NewWarningForNode(t.sourceFile, node, code, message))
}

// setNodePos copies the source position from a TS node onto a Lua node for source map generation.
func (t *Transpiler) setNodePos(luaNode lua.Positioned, tsNode *ast.Node) {
	if tsNode == nil || !t.sourceMapEnabled {
		return
	}
	// SkipTrivia advances past leading whitespace/comments to the actual token start.
	pos := scanner.SkipTrivia(t.sourceFile.Text(), tsNode.Pos())
	line, col := scanner.GetECMALineAndUTF16CharacterOfPosition(t.sourceFile, pos)
	luaNode.SetSourcePos(line, int(col))
}

// setNodePosNamed sets source position and original name on a Lua node.
// Used when an identifier is renamed (e.g. reserved word "type" → "____type").
func (t *Transpiler) setNodePosNamed(luaNode lua.Positioned, tsNode *ast.Node, originalName string) {
	t.setNodePos(luaNode, tsNode)
	if !t.sourceMapEnabled || tsNode == nil {
		return
	}
	if id, ok := luaNode.(*lua.Identifier); ok {
		id.Pos.SourceName = originalName
	}
}

// nextTemp generates a unique temporary variable name.
func (t *Transpiler) nextTemp(prefix string) string {
	if prefix == "" {
		prefix = "temp"
	}
	prefix = strings.TrimLeft(prefix, "_")
	t.tempCount++
	return fmt.Sprintf("____%s_%d", prefix, t.tempCount-1)
}

// pushPrecedingStatements creates a new scope for accumulating expression setup statements.
func (t *Transpiler) pushPrecedingStatements() {
	t.precedingStatementsStack = append(t.precedingStatementsStack, nil)
}

// popPrecedingStatements removes and returns the current scope's accumulated statements.
func (t *Transpiler) popPrecedingStatements() []lua.Statement {
	n := len(t.precedingStatementsStack)
	stmts := t.precedingStatementsStack[n-1]
	t.precedingStatementsStack = t.precedingStatementsStack[:n-1]
	return stmts
}

// addPrecedingStatements adds setup statements for the current expression transformation.
// A scope must be active (pushed by pushPrecedingStatements or transformExprInScope).
func (t *Transpiler) addPrecedingStatements(stmts ...lua.Statement) {
	n := len(t.precedingStatementsStack)
	if n == 0 {
		panic("addPrecedingStatements called without an active scope")
	}
	t.precedingStatementsStack[n-1] = append(t.precedingStatementsStack[n-1], stmts...)
}

// precedingStatementsLen returns the current number of preceding statements in the active scope.
func (t *Transpiler) precedingStatementsLen() int {
	n := len(t.precedingStatementsStack)
	if n == 0 {
		return 0
	}
	return len(t.precedingStatementsStack[n-1])
}

// insertPrecedingStatements inserts statements at a given position in the active scope.
func (t *Transpiler) insertPrecedingStatements(pos int, stmts ...lua.Statement) {
	n := len(t.precedingStatementsStack)
	if n == 0 {
		panic("insertPrecedingStatements called without an active scope")
	}
	cur := t.precedingStatementsStack[n-1]
	result := make([]lua.Statement, 0, len(cur)+len(stmts))
	result = append(result, cur[:pos]...)
	result = append(result, stmts...)
	result = append(result, cur[pos:]...)
	t.precedingStatementsStack[n-1] = result
}

// transformExprInScope transforms an expression in a new preceding statement scope,
// capturing any setup statements the expression generates.
func (t *Transpiler) transformExprInScope(node *ast.Node) (lua.Expression, []lua.Statement) {
	t.pushPrecedingStatements()
	expr := t.transformExpression(node)
	stmts := t.popPrecedingStatements()
	return expr, stmts
}

// hasLocalBinding returns true if an identifier has a user-defined local declaration
// (not a global built-in). Used to avoid replacing `const Infinity = 1` with `math.huge`.
func (t *Transpiler) hasLocalBinding(node *ast.Node) bool {

	sym := t.checker.GetSymbolAtLocation(node)
	if sym == nil {
		return false
	}
	// Check if any declaration is non-ambient (user-defined, not from lib.d.ts)
	for _, decl := range sym.Declarations {
		// ShorthandPropertyAssignment creates a property symbol, not a real variable binding
		if decl.Kind == ast.KindShorthandPropertyAssignment {
			continue
		}
		if ast.GetCombinedModifierFlags(decl)&ast.ModifierFlagsAmbient == 0 {
			sf := ast.GetSourceFileOfNode(decl)
			if sf != nil && !sf.IsDeclarationFile {
				return true
			}
		}
	}
	return false
}

// isFirstDeclaration returns true if node is the first (or only) declaration of its symbol.
// Used for enum/namespace merging: only the first declaration initializes the table.
func (t *Transpiler) isFirstDeclaration(node *ast.Node, nameNode *ast.Node) bool {

	sym := t.checker.GetSymbolAtLocation(nameNode)
	if sym == nil {
		return true
	}
	// Empty namespaces have no ValueDeclaration (no runtime value).
	// Treat them as first declaration so the table gets created.
	if sym.ValueDeclaration == nil {
		return true
	}
	return sym.ValueDeclaration == node
}

// allDeclarationsInCurrentFile returns true if every declaration of the symbol
// is in the current source file. When true, merged enums/namespaces don't need
// the `X = X or ({})` global-read pattern — a plain `local X = {}` suffices.
func (t *Transpiler) allDeclarationsInCurrentFile(nameNode *ast.Node) bool {

	sym := t.checker.GetSymbolAtLocation(nameNode)
	if sym == nil {
		return true
	}
	for _, decl := range sym.Declarations {
		sf := ast.GetSourceFileOfNode(decl)
		if sf != t.sourceFile {
			return false
		}
	}
	return true
}

// collectCrossFileEnums scans all source files for top-level enum declarations
// and returns the set of enum names that appear in 2+ files. These enums need
// the global `Foo = Foo or ({})` pattern for cross-file merging to work.
func collectCrossFileEnums(program *compiler.Program) map[string]bool {
	// enum name → number of distinct files declaring it
	counts := map[string]int{}
	for _, sf := range program.SourceFiles() {
		if isDeclarationFile(sf.FileName()) {
			continue
		}
		seen := map[string]bool{} // dedup within one file
		for _, stmt := range sf.Statements.Nodes {
			if stmt.Kind == ast.KindEnumDeclaration {
				name := stmt.AsEnumDeclaration().Name().AsIdentifier().Text
				if !seen[name] {
					seen[name] = true
					counts[name]++
				}
			}
		}
	}
	result := map[string]bool{}
	for name, count := range counts {
		if count >= 2 {
			result[name] = true
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ModuleDependency describes a single require() emitted by a transpiled file.
// Populated during transformation by resolveModulePath.
type ModuleDependency struct {
	// RequirePath is the dot-separated path in the emitted require("...").
	RequirePath string
	// ResolvedPath is the absolute filesystem path of the resolved module.
	// Empty if resolution failed or the specifier was in noResolvePaths.
	ResolvedPath string
	// IsExternal is true when the resolved file lives outside the project
	// source root (e.g. node_modules).
	IsExternal bool
	// IsLuaSource is true when the resolved file is a .lua file (not
	// transpiled from .ts/.tsx).
	IsLuaSource bool
}

// TranspileResult contains the Lua output for a single source file.
type TranspileResult struct {
	FileName        string
	Lua             string
	SourceMap       string // V3 source map JSON (empty if source maps disabled)
	Declaration     string // .d.ts content (empty if declaration emission disabled)
	UsesLualib      bool
	UsesMiddleclass bool               // file emits require("middleclass"); tooling should make the module resolvable
	ExportedNames   []string           // names exported by this file (populated when ExportAsGlobal is set)
	LualibDeps      []string           // lualib features used by this file (populated when NoLualibImport is set)
	Dependencies    []ModuleDependency // module dependencies discovered during transformation
	TransformDur    time.Duration      // time spent transforming TS AST → Lua AST
	PrintDur        time.Duration      // time spent printing Lua AST → string
}

type declarationEmit struct {
	text  string
	diags []*ast.Diagnostic
}

func collectDeclarationEmits(program *compiler.Program, onlyFiles map[string]bool) map[string]declarationEmit {
	options := program.Options()
	if options == nil || !options.GetEmitDeclarations() {
		return nil
	}

	emits := make(map[string]declarationEmit)
	for _, sf := range program.SourceFiles() {
		fileName := sf.FileName()
		if isDeclarationFile(fileName) || (onlyFiles != nil && !onlyFiles[fileName]) {
			continue
		}

		var decl declarationEmit
		program.Emit(context.Background(), compiler.EmitOptions{
			TargetSourceFile: sf,
			EmitOnly:         compiler.EmitOnlyDts,
			WriteFile: func(fileName string, text string, data *compiler.WriteFileData) error {
				if !strings.HasSuffix(fileName, ".d.ts") {
					return nil
				}
				decl.text = text
				if data != nil && len(data.Diagnostics) > 0 {
					decl.diags = append(decl.diags, data.Diagnostics...)
				}
				return nil
			},
		})
		if decl.text != "" || len(decl.diags) > 0 {
			emits[fileName] = decl
		}
	}
	return emits
}

// TranspileOptions holds configuration for transpilation.
type TranspileOptions struct {
	EmitMode                  EmitMode
	ExportAsGlobal            bool                    // strip module wrapper, emit exports as globals
	ExportAsGlobalMatch       string                  // regex: only apply ExportAsGlobal to files whose source path matches
	NoImplicitSelf            bool                    // unresolved calls default to no-self
	NoImplicitGlobalVariables bool                    // force local declarations in script-mode top-level scope
	LuaLibImport              LuaLibImportKind        // how lualib features are included (default: require)
	LualibInlineContent       string                  // lualib bundle content for inline mode (full bundle fallback)
	LualibFeatureData         *lualibinfo.FeatureData // per-feature data for selective inline mode
	SourceMap                 bool                    // generate source maps
	SourceMapTraceback        bool                    // register source maps at runtime for debug.traceback rewriting
	InlineSourceMap           bool                    // embed source map as base64 data: URL in Lua output
	Trace                     bool                    // emit --[[trace: ...]] comments showing which TS node produced each Lua statement
	ClassStyle                ClassStyle              // alternative class emit style (default: TSTL prototype chains)
	NoResolvePaths            []string                // module specifiers to emit as-is without resolving (TSTL noResolvePaths)
}

// TranspileProgram transpiles all user source files in the program.
// sourceRoot is the root directory for computing relative Lua module paths.
// If onlyFiles is non-nil, only files in the set are transpiled.
func TranspileProgram(program *compiler.Program, sourceRoot string, luaTarget LuaTarget, onlyFiles map[string]bool, emitMode ...EmitMode) ([]TranspileResult, []*ast.Diagnostic) {
	opts := TranspileOptions{EmitMode: EmitModeTSTL}
	if len(emitMode) > 0 && emitMode[0] != "" {
		opts.EmitMode = emitMode[0]
	}
	return TranspileProgramWithOptions(program, sourceRoot, luaTarget, onlyFiles, opts)
}

// TranspileProgramWithOptions transpiles all user source files with full options.
func TranspileProgramWithOptions(program *compiler.Program, sourceRoot string, luaTarget LuaTarget, onlyFiles map[string]bool, opts TranspileOptions) ([]TranspileResult, []*ast.Diagnostic) {
	if opts.EmitMode == "" {
		opts.EmitMode = EmitModeTSTL
	}
	if opts.LuaLibImport == "" {
		opts.LuaLibImport = LuaLibImportRequire
	}
	var ch *checker.Checker
	compiler.Program_ForEachCheckerParallel(program, func(_ int, c *checker.Checker) {
		ch = c
	})

	// Trigger semantic analysis so the checker populates alias reference data
	// (needed for import elision via IsReferencedAliasDeclaration).
	if onlyFiles != nil {
		// Incremental: only check files we're actually transpiling.
		for _, sf := range program.SourceFiles() {
			if onlyFiles[sf.FileName()] {
				compiler.Program_GetSemanticDiagnostics(program, context.Background(), sf)
			}
		}
	} else {
		compiler.Program_GetSemanticDiagnostics(program, context.Background(), nil)
	}

	crossFileEnums := collectCrossFileEnums(program)
	declarationEmits := collectDeclarationEmits(program, onlyFiles)

	// Compile ExportAsGlobalMatch regex once for per-file matching.
	var exportAsGlobalRe *regexp.Regexp
	if opts.ExportAsGlobalMatch != "" {
		exportAsGlobalRe = regexp.MustCompile(opts.ExportAsGlobalMatch)
	}

	var noResolvePathsSet map[string]bool
	if len(opts.NoResolvePaths) > 0 {
		noResolvePathsSet = make(map[string]bool, len(opts.NoResolvePaths))
		for _, p := range opts.NoResolvePaths {
			noResolvePathsSet[p] = true
		}
	}

	var results []TranspileResult
	var diagnostics []*ast.Diagnostic
	for _, sf := range program.SourceFiles() {
		fileName := sf.FileName()
		if isDeclarationFile(fileName) {
			continue
		}
		if onlyFiles != nil && !onlyFiles[fileName] {
			continue
		}

		exportAsGlobal := opts.ExportAsGlobal
		if exportAsGlobalRe != nil {
			exportAsGlobal = exportAsGlobalRe.MatchString(fileName)
		}

		if ch == nil {
			panic("transpiler requires a non-nil checker")
		}

		isModule := isExternalModule(sf)
		t := &Transpiler{
			program:                   program,
			checker:                   ch,
			sourceFile:                sf,
			sourceRoot:                sourceRoot,
			luaTarget:                 luaTarget,
			emitMode:                  opts.EmitMode,
			exportAsGlobal:            exportAsGlobal,
			luaLibImport:              opts.LuaLibImport,
			lualibInlineContent:       opts.LualibInlineContent,
			lualibFeatureData:         opts.LualibFeatureData,
			noImplicitSelf:            opts.NoImplicitSelf,
			noImplicitGlobalVariables: opts.NoImplicitGlobalVariables,
			classStyle:                opts.ClassStyle,
			noResolvePaths:            noResolvePathsSet,
			crossFileEnums:            crossFileEnums,
			compilerOptions:           program.Options(),
			isModule:                  isModule,
			isStrict:                  isModule, // ES modules are always strict; matches TSTL context.isStrict
			sourceMapEnabled:          opts.SourceMap,
			traceEnabled:              opts.Trace,
			removeComments:            program.Options() != nil && program.Options().RemoveComments.IsTrue(),
		}
		// sourceMapTraceback requires the lualib feature — must be registered
		// before transformation so the import gets included in the AST output.
		if opts.SourceMapTraceback {
			t.requireLualib("__TS__SourceMapTraceBack")
		}

		tTransform := time.Now()
		luaAST := t.transformSourceFileAST(sf)
		transformDur := time.Since(tTransform)

		// Insert a sentinel statement for sourceMapTraceback. The sentinel occupies
		// one line in the printed output; after printing and computing the source map,
		// we replace it with the actual traceback call. This avoids fragile string
		// matching on lualib output format to find the insertion point.
		if opts.SourceMapTraceback && opts.SourceMap {
			luaAST = insertTracebackSentinel(luaAST)
		}

		// Extract shebang trivia to prepend after printing.
		shebang := shebangRe.FindString(sf.Text())

		tPrint := time.Now()
		var luaCode string
		var sourceMapJSON string
		if opts.SourceMap {
			printResult := lua.PrintStatementsWithSourceMap(luaAST, t.luaTarget.AllowsUnicodeIds())
			luaCode = printResult.Code
			sourceMapJSON = t.buildSourceMap(fileName, sf, printResult.Mappings)

			if opts.SourceMapTraceback {
				luaCode = replaceTracebackSentinel(luaCode, printResult.Mappings)
			}

			if opts.InlineSourceMap {
				inlineComment := "--# sourceMappingURL=data:application/json;base64," +
					base64.StdEncoding.EncodeToString([]byte(sourceMapJSON)) + "\n"
				luaCode += "\n" + inlineComment
			}
		} else {
			luaCode = lua.PrintStatements(luaAST, t.luaTarget.AllowsUnicodeIds())
		}
		if shebang != "" {
			luaCode = shebang + luaCode
		}
		printDur := time.Since(tPrint)

		var exportNames []string
		if exportAsGlobal && t.exportedNames != nil {
			for name := range t.exportedNames {
				exportNames = append(exportNames, name)
			}
			slices.Sort(exportNames)
		}
		var lualibDeps []string
		if len(t.lualibs) > 0 &&
			(opts.LuaLibImport == LuaLibImportNone || opts.LuaLibImport == LuaLibImportRequireMinimal) {
			lualibDeps = append([]string{}, t.lualibs...)
		}
		var declaration string
		if decl, ok := declarationEmits[fileName]; ok {
			declaration = t.postProcessDeclaration(decl.text)
			diagnostics = append(diagnostics, decl.diags...)
		}
		results = append(results, TranspileResult{
			FileName:        fileName,
			Lua:             luaCode,
			SourceMap:       sourceMapJSON,
			Declaration:     declaration,
			UsesLualib:      len(t.lualibs) > 0,
			UsesMiddleclass: t.usesMiddleclass,
			ExportedNames:   exportNames,
			LualibDeps:      lualibDeps,
			Dependencies:    t.dependencies,
			TransformDur:    transformDur,
			PrintDur:        printDur,
		})
		diagnostics = append(diagnostics, t.diagnostics...)
	}
	return results, diagnostics
}

// buildSourceMap converts printer mappings into a V3 source map JSON string.
func (t *Transpiler) buildSourceMap(fileName string, sf *ast.SourceFile, mappings []lua.Mapping) string {
	luaFile := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName)) + ".lua"

	// Normalize sourceRoot: strip trailing slashes, append "/"
	var sourceRoot string
	if t.compilerOptions != nil && t.compilerOptions.SourceRoot != "" {
		sr := t.compilerOptions.SourceRoot
		sr = strings.TrimRight(sr, "/\\")
		sourceRoot = sr + "/"
	}
	gen := sourcemap.NewGenerator(luaFile, sourceRoot)

	// Compute relative path from output directory to source file.
	// With outDir, the .lua file lands in outDir (preserving directory structure
	// relative to rootDir), so we need the path from there back to the source.
	srcPath := t.sourceMapRelativePath(fileName)
	srcIdx := gen.AddSource(srcPath)
	gen.SetSourceContent(srcIdx, sf.Text())

	for _, m := range mappings {
		if m.Name != "" {
			gen.AddNamedMapping(m.GenLine, m.GenCol, srcIdx, m.SrcLine, m.SrcCol, m.Name)
		} else {
			gen.AddMapping(m.GenLine, m.GenCol, srcIdx, m.SrcLine, m.SrcCol)
		}
	}
	return gen.String()
}

// sourceMapRelativePath computes the relative path from the output .lua file's
// directory back to the source .ts file. This matches TSTL's behavior:
//
//	path.relative(path.dirname(luaOutputPath), tsSourcePath)
//
// With outDir/rootDir, the .lua file's directory changes, so the relative path
// adjusts accordingly (e.g. "../src/foo.ts" when outDir="dst" and source is in "src/").
func (t *Transpiler) sourceMapRelativePath(fileName string) string {
	// Determine the output directory for this file.
	// Without outDir, source and output are in the same directory → just basename.
	outDir := ""
	rootDir := ""
	if t.compilerOptions != nil {
		outDir = t.compilerOptions.OutDir
		rootDir = t.compilerOptions.RootDir
	}

	if outDir == "" {
		return filepath.Base(fileName)
	}

	// Resolve outDir relative to sourceRoot (project root)
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(t.sourceRoot, outDir)
	}

	// Compute the subdirectory structure: strip rootDir (or sourceRoot) prefix from fileName.
	// e.g. fileName="/proj/src/sub/foo.ts", rootDir="/proj/src" → subPath="sub/foo.ts"
	baseDir := t.sourceRoot
	if rootDir != "" {
		if !filepath.IsAbs(rootDir) {
			baseDir = filepath.Join(t.sourceRoot, rootDir)
		} else {
			baseDir = rootDir
		}
	} else if outDir != "" {
		// When outDir is set without rootDir, TypeScript uses the common root
		// of all source files. Use the source file's directory as approximation.
		baseDir = filepath.Dir(fileName)
	}
	subPath, err := filepath.Rel(baseDir, fileName)
	if err != nil {
		return filepath.Base(fileName)
	}

	// The output file would be at: outDir/subPath (with .lua extension).
	// We want: relative(dirname(outDir/subPath), fileName)
	outputFile := filepath.Join(outDir, subPath)
	rel, err := filepath.Rel(filepath.Dir(outputFile), fileName)
	if err != nil {
		return filepath.Base(fileName)
	}
	return filepath.ToSlash(rel)
}

// sourceMapTracebackSentinel is a placeholder comment inserted into the Lua AST
// before printing. After printing and computing source mappings, it is replaced
// with the real __TS__SourceMapTraceBack call. Using a sentinel avoids fragile
// string matching on lualib output format and keeps line numbers stable (the
// sentinel and its replacement each occupy exactly one line).
const sourceMapTracebackSentinel = "--[[__SOURCEMAP_TRACEBACK__]]"

// insertTracebackSentinel inserts the sentinel after the last lualib-related
// statement in the AST (import block or inline block). If no lualib statements
// exist, it inserts at position 0.
func insertTracebackSentinel(stmts []lua.Statement) []lua.Statement {
	// Find insertion point: after the last lualib-related statement.
	// Lualib statements are always at the front (prepended by transformSourceFile).
	insertAt := 0
	for i, stmt := range stmts {
		switch s := stmt.(type) {
		case *lua.RawStatement:
			if strings.Contains(s.Code, "Lua Library inline imports") {
				insertAt = i + 1
			}
		case *lua.VariableDeclarationStatement:
			if len(s.Left) > 0 && (s.Left[0].Text == "____lualib" || strings.HasPrefix(s.Left[0].Text, "__TS__")) {
				insertAt = i + 1
			}
		}
	}
	sentinel := lua.RawStmt(sourceMapTracebackSentinel)
	result := make([]lua.Statement, 0, len(stmts)+1)
	result = append(result, stmts[:insertAt]...)
	result = append(result, sentinel)
	result = append(result, stmts[insertAt:]...)
	return result
}

// replaceTracebackSentinel finds the sentinel line in the printed output and
// replaces it with the real __TS__SourceMapTraceBack(...) call built from the
// source mappings. Since the replacement is one line for one line, all other
// line numbers remain stable.
func replaceTracebackSentinel(luaCode string, mappings []lua.Mapping) string {
	lines := strings.Split(luaCode, "\n")

	// Find the sentinel line
	sentinelLine := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == sourceMapTracebackSentinel {
			sentinelLine = i
			break
		}
	}
	if sentinelLine < 0 {
		return luaCode // no sentinel found, return as-is
	}

	// Build the traceback call. Source map lines are 0-based;
	// the Lua runtime traceback table uses 1-based lines.
	lineMap := make(map[int]int)
	for _, m := range mappings {
		genLine1 := m.GenLine + 1
		srcLine1 := m.SrcLine + 1
		if existing, ok := lineMap[genLine1]; !ok || srcLine1 < existing {
			lineMap[genLine1] = srcLine1
		}
	}

	keys := make([]int, 0, len(lineMap))
	for k := range lineMap {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var b strings.Builder
	b.WriteString("__TS__SourceMapTraceBack(debug.getinfo(1).short_src, {")
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "[%q] = %d", strconv.Itoa(k), lineMap[k])
	}
	b.WriteString("});")

	lines[sentinelLine] = b.String()
	return strings.Join(lines, "\n")
}

// lualibFeatureExports maps each multi-export lualib feature to its full export list.
// When any export in the group is requested, all exports are imported together (matching TSTL).
// Single-export features don't need entries here.
var lualibFeatureExports = map[string][]string{
	"__TS__AsyncAwaiter":         {"__TS__AsyncAwaiter", "__TS__Await"},
	"__TS__Await":                {"__TS__AsyncAwaiter", "__TS__Await"},
	"Error":                      {"Error", "RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError"},
	"RangeError":                 {"Error", "RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError"},
	"ReferenceError":             {"Error", "RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError"},
	"SyntaxError":                {"Error", "RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError"},
	"TypeError":                  {"Error", "RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError"},
	"URIError":                   {"Error", "RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError"},
	"__TS__Symbol":               {"__TS__Symbol", "Symbol"},
	"Symbol":                     {"__TS__Symbol", "Symbol"},
	"__TS__SymbolRegistryFor":    {"__TS__SymbolRegistryFor", "__TS__SymbolRegistryKeyFor"},
	"__TS__SymbolRegistryKeyFor": {"__TS__SymbolRegistryFor", "__TS__SymbolRegistryKeyFor"},
}

// requireLualib registers a lualib function and returns its local name.
// For multi-export features, all exports in the feature group are imported together.
func (t *Transpiler) requireLualib(name string) string {
	if t.lualibSet[name] {
		return name
	}
	if t.lualibSet == nil {
		t.lualibSet = make(map[string]bool)
	}
	if exports, ok := lualibFeatureExports[name]; ok {
		for _, exp := range exports {
			if !t.lualibSet[exp] {
				t.lualibSet[exp] = true
				t.lualibs = append(t.lualibs, exp)
			}
		}
	} else {
		t.lualibSet[name] = true
		t.lualibs = append(t.lualibs, name)
	}
	return name
}

// unpackIdent returns the target-appropriate unpack function expression.
// Lua 5.0/5.1/LuaJIT: unpack, Lua 5.2+: table.unpack, Universal: __TS__Unpack
func (t *Transpiler) unpackIdent() lua.Expression {
	if t.luaTarget == LuaTargetUniversal {
		fn := t.requireLualib("__TS__Unpack")
		return lua.Ident(fn)
	}
	if t.luaTarget.UsesTableUnpack() {
		return lua.Index(lua.Ident("table"), lua.Str("unpack"))
	}
	return lua.Ident("unpack")
}

// unpackCall returns a target-appropriate unpack(expr, 1, N) call.
// For Lua 5.0, bounds args are omitted. For Universal, uses __TS__Unpack.
func (t *Transpiler) unpackCall(expr lua.Expression, count int) lua.Expression {
	fn := t.unpackIdent()
	if t.luaTarget.UsesLua50Unpack() || t.luaTarget == LuaTargetUniversal {
		return lua.Call(fn, expr)
	}
	return lua.Call(fn, expr, lua.Num("1"), lua.Num(fmt.Sprintf("%d", count)))
}

func isDeclarationFile(name string) bool {
	return strings.HasSuffix(name, ".d.ts")
}

func isExternalModule(sf *ast.SourceFile) bool {
	// Use tsgo's ExternalModuleIndicator which respects moduleDetection: "force",
	// matching TypeScript's ts.isExternalModule() that TSTL uses.
	return sf.ExternalModuleIndicator != nil
}

// hasExportEquals checks if the source file contains an `export = expr` statement.
// When true, the module init skips `local ____exports = {}` because the export
// assignment will provide the initializer directly.
// Ported from: src/transformation/utils/typescript/index.ts (hasExportEquals)
func hasExportEquals(sf *ast.SourceFile) bool {
	for _, stmt := range sf.Statements.Nodes {
		if stmt.Kind == ast.KindExportAssignment && stmt.AsExportAssignment().IsExportEquals {
			return true
		}
	}
	return false
}

func hasExportModifier(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, m := range modifiers.Nodes {
		if m.Kind == ast.KindExportKeyword {
			return true
		}
	}
	return false
}

func hasDefaultModifier(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, m := range modifiers.Nodes {
		if m.Kind == ast.KindDefaultKeyword {
			return true
		}
	}
	return false
}

func hasDeclareModifier(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, m := range modifiers.Nodes {
		if m.Kind == ast.KindDeclareKeyword {
			return true
		}
	}
	return false
}

// transformSourceFileAST processes the top-level statements of a source file and
// returns the Lua AST (without printing). Use lua.PrintStatements to render.
func (t *Transpiler) transformSourceFileAST(sf *ast.SourceFile) []lua.Statement {
	// JSON files: transform the single expression and wrap in return.
	if sf.AsNode().Flags&ast.NodeFlagsJsonFile != 0 {
		return t.transformJSONSourceFile(sf)
	}

	var stmts []lua.Statement

	// When the file has `export = expr`, the export assignment handler emits
	// `____exports = expr` which becomes the local declaration. Skip the
	// empty table init so we get `local ____exports = expr` instead of
	// `local ____exports = {} ... ____exports = expr`.
	// Ported from: src/transformation/utils/typescript/index.ts (hasExportEquals)
	if t.isModule && !t.exportAsGlobal && !hasExportEquals(sf) {
		stmts = append(stmts, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident("____exports")},
			[]lua.Expression{lua.Table()},
		))
	}
	if t.isModule {
		t.collectExportedNames(sf)
	}

	// Push file scope for symbol tracking and hoisting
	sfNode := sf.AsNode()
	scope := t.pushScope(ScopeFile, sfNode)

	var bodyStmts []lua.Statement

	// Check for `using` declarations at root level (no return from using calls)
	if blockHasUsingDeclaration(sf.Statements) {
		_, bodyStmts = t.transformStatementsWithUsing(sf.Statements.Nodes, false)
	} else {
		for _, stmt := range sf.Statements.Nodes {
			bodyStmts = append(bodyStmts, t.transformStatementWithComments(stmt)...)
		}
	}

	// Perform scope-based hoisting on transformed statements
	bodyStmts = t.performHoisting(scope, bodyStmts)
	t.popScope()

	stmts = append(stmts, bodyStmts...)

	if t.isModule && !t.exportAsGlobal {
		stmts = append(stmts, lua.Return(lua.Ident("____exports")))
	}

	// Prepend lualib imports if any were used
	if len(t.lualibs) > 0 && t.luaLibImport != LuaLibImportNone {
		var header []lua.Statement
		switch t.luaLibImport {
		case LuaLibImportInline:
			var inlineCode string
			if t.lualibFeatureData != nil {
				inlineCode = t.lualibFeatureData.ResolveInlineCode(t.lualibs)
			} else {
				inlineCode = t.lualibInlineContent
			}
			header = append(header,
				lua.RawStmt("-- Lua Library inline imports\n"+inlineCode+"\n-- End of Lua Library inline imports"),
			)
		default: // LuaLibImportRequire, LuaLibImportRequireMinimal (and default)
			header = append(header, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident("____lualib")},
				[]lua.Expression{lua.Call(lua.Ident("require"), lua.Str("lualib_bundle"))},
			))
			for _, lib := range t.lualibs {
				header = append(header, lua.LocalDecl(
					[]*lua.Identifier{lua.Ident(lib)},
					[]lua.Expression{lua.Index(lua.Ident("____lualib"), lua.Str(lib))},
				))
			}
		}
		stmts = append(header, stmts...)
	}

	// Prepend `local class = require("middleclass")` when the middleclass
	// class style actually emitted at least one class. Parallels the lualib
	// require header above. Placed after the lualib header so both imports
	// live at the top of the file.
	if t.usesMiddleclass {
		stmts = append([]lua.Statement{
			lua.LocalDecl(
				[]*lua.Identifier{lua.Ident("class")},
				[]lua.Expression{lua.Call(lua.Ident("require"), lua.Str("middleclass"))},
			),
		}, stmts...)
	}

	return stmts
}

// transformJSONSourceFile handles JSON source files by transforming the single
// expression statement and wrapping it in a return.
func (t *Transpiler) transformJSONSourceFile(sf *ast.SourceFile) []lua.Statement {
	if sf.Statements == nil || len(sf.Statements.Nodes) == 0 {
		// Empty JSON file → error("Unexpected end of JSON input")
		return []lua.Statement{
			lua.ExprStmt(lua.Call(lua.Ident("error"), lua.Str("Unexpected end of JSON input"))),
		}
	}
	stmt := sf.Statements.Nodes[0]
	expr, precedingStmts := t.transformExprInScope(stmt.AsExpressionStatement().Expression)
	var stmts []lua.Statement
	stmts = append(stmts, precedingStmts...)
	stmts = append(stmts, lua.Return(expr))
	return stmts
}

// transformStatement dispatches a statement node to the appropriate handler.
func (t *Transpiler) transformStatement(node *ast.Node) (result []lua.Statement) {
	// Set source position on the first result statement for source map generation.
	// Only sets position if the transform function didn't already set one (fallback).
	if t.sourceMapEnabled {
		defer func() {
			if len(result) > 0 {
				if n, ok := result[0].(lua.Node); ok && !n.SourcePosition().HasPos {
					if p, ok := result[0].(lua.Positioned); ok {
						t.setNodePos(p, node)
					}
				}
			}
		}()
	}

	// Emit trace comments showing which TS node produced each Lua statement.
	if t.traceEnabled {
		defer func() {
			if len(result) > 0 {
				comment := fmt.Sprintf("--[[trace: %s]]", node.Kind.String())
				c := result[0].(interface{ GetComments() *lua.Comments }).GetComments()
				c.LeadingComments = append([]string{comment}, c.LeadingComments...)
			}
		}()
	}

	// Ambient declarations (declare const, declare function, etc.) are erased
	if hasDeclareModifier(node) {
		return nil
	}

	switch node.Kind {
	case ast.KindVariableStatement:
		return t.transformVariableStatement(node)
	case ast.KindExpressionStatement:
		return t.transformExpressionStatement(node)
	case ast.KindFunctionDeclaration:
		return t.transformFunctionDeclaration(node)
	case ast.KindReturnStatement:
		return t.transformReturnStatement(node)
	case ast.KindIfStatement:
		return t.transformIfStatement(node)
	case ast.KindBlock:
		// Standalone blocks create a new scope via do...end
		bodyStmts := t.transformBlock(node)
		return []lua.Statement{lua.Do(bodyStmts...)}
	case ast.KindWhileStatement:
		return t.transformWhileStatement(node)
	case ast.KindDoStatement:
		return t.transformDoWhileStatement(node)
	case ast.KindForStatement:
		return t.transformForStatement(node)
	case ast.KindForOfStatement:
		return t.transformForOfStatement(node)
	case ast.KindForInStatement:
		return t.transformForInStatement(node)
	case ast.KindEnumDeclaration:
		return t.transformEnumDeclaration(node)
	case ast.KindImportDeclaration:
		return t.transformImportDeclaration(node)
	case ast.KindImportEqualsDeclaration:
		return t.transformImportEqualsDeclaration(node)
	case ast.KindExportDeclaration:
		return t.transformExportDeclaration(node)
	case ast.KindExportAssignment:
		return t.transformExportAssignment(node)
	case ast.KindClassDeclaration:
		return t.transformClassDeclaration(node)
	case ast.KindModuleDeclaration:
		return t.transformModuleDeclaration(node)
	case ast.KindSwitchStatement:
		return t.transformSwitchStatement(node)
	case ast.KindTryStatement:
		return t.transformTryStatement(node)
	case ast.KindThrowStatement:
		return t.transformThrowStatement(node)
	case ast.KindBreakStatement:
		bs := node.AsBreakStatement()
		tsLabel := ""
		if bs.Label != nil {
			tsLabel = bs.Label.Text()
		}
		return t.buildBreakDispatch(tsLabel)
	case ast.KindContinueStatement:
		cs := node.AsContinueStatement()
		tsLabel := ""
		if cs.Label != nil {
			tsLabel = cs.Label.Text()
		}
		return t.buildContinueDispatch(tsLabel)
	case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
		// Type-only declarations — erased in Lua output
		return nil
	case ast.KindEmptyStatement:
		return nil
	case ast.KindLabeledStatement:
		return t.transformLabeledStatement(node)
	default:
		t.addWarning(node, dw.UnsupportedNodeKind, fmt.Sprintf("Unsupported node kind %s", strings.TrimPrefix(node.Kind.String(), "Kind")))
		return nil
	}
}

// identName extracts the string name from a Lua identifier expression.
// Panics if expr is not an *lua.Identifier — callers must ensure this.
func identName(expr lua.Expression) string {
	return expr.(*lua.Identifier).Text
}

// ==========================================================================
// Scope stack management
// ==========================================================================

// pushScope creates a new scope and pushes it onto the scope stack.
func (t *Transpiler) pushScope(scopeType ScopeType, node *ast.Node) *Scope {
	t.lastScopeID++
	scope := &Scope{
		Type: scopeType,
		ID:   t.lastScopeID,
		Node: node,
	}
	t.scopeStack = append(t.scopeStack, scope)
	return scope
}

// popScope removes and returns the top scope from the scope stack.
func (t *Transpiler) popScope() *Scope {
	n := len(t.scopeStack)
	scope := t.scopeStack[n-1]
	t.scopeStack = t.scopeStack[:n-1]
	return scope
}

// peekScope returns the current (top) scope.
func (t *Transpiler) peekScope() *Scope {
	return t.scopeStack[len(t.scopeStack)-1]
}

// findTryScopeInStack walks up the scope stack looking for an enclosing
// Try/Catch scope. Returns nil if a Function scope is encountered first (i.e.
// the try belongs to an outer function, so a return inside our function body
// doesn't cross its pcall/awaiter boundary). Used by return handling.
func (t *Transpiler) findTryScopeInStack() *Scope {
	for i := len(t.scopeStack) - 1; i >= 0; i-- {
		s := t.scopeStack[i]
		if s.Type == ScopeFunction {
			return nil
		}
		if s.Type == ScopeTry || s.Type == ScopeCatch {
			return s
		}
	}
	return nil
}

// findTryScopeAbove returns the innermost Try/Catch scope above the given
// scope-stack index, or nil if none (or a Function scope intervenes). Used by
// break/continue handling to detect whether the jump crosses a pcall/awaiter
// function boundary.
func (t *Transpiler) findTryScopeAbove(targetDepth int) *Scope {
	for i := len(t.scopeStack) - 1; i > targetDepth; i-- {
		s := t.scopeStack[i]
		if s.Type == ScopeFunction {
			return nil
		}
		if s.Type == ScopeTry || s.Type == ScopeCatch {
			return s
		}
	}
	return nil
}

// findInnermostBreakTargetDepth returns the scope-stack index of the innermost
// Loop or Switch scope inside the current function. -1 if none.
func (t *Transpiler) findInnermostBreakTargetDepth() int {
	for i := len(t.scopeStack) - 1; i >= 0; i-- {
		s := t.scopeStack[i]
		if s.Type == ScopeFunction {
			return -1
		}
		if s.Type == ScopeLoop || s.Type == ScopeSwitch {
			return i
		}
	}
	return -1
}

// findInnermostLoopDepth returns the scope-stack index of the innermost Loop
// scope inside the current function. -1 if none.
func (t *Transpiler) findInnermostLoopDepth() int {
	for i := len(t.scopeStack) - 1; i >= 0; i-- {
		s := t.scopeStack[i]
		if s.Type == ScopeFunction {
			return -1
		}
		if s.Type == ScopeLoop {
			return i
		}
	}
	return -1
}

// addDeferredTransfer appends a transfer to the try scope's list, deduping by
// flag name.
func (s *Scope) addDeferredTransfer(xfer DeferredTransfer) {
	for _, existing := range s.TryDeferredTransfers {
		if existing.FlagName == xfer.FlagName {
			return
		}
	}
	s.TryDeferredTransfers = append(s.TryDeferredTransfers, xfer)
}

// buildBreakDispatch emits the Lua statements for a TS break statement,
// routing through the sentinel-flag path when the break would cross a
// pcall/awaiter function boundary from within a try/catch body.
func (t *Transpiler) buildBreakDispatch(tsLabel string) []lua.Statement {
	targetDepth := -1
	if tsLabel != "" {
		if d, ok := t.labelScopeDepths[tsLabel]; ok {
			targetDepth = d - 1
		}
	} else {
		targetDepth = t.findInnermostBreakTargetDepth()
	}

	normal := t.normalBreakStatements(tsLabel)

	if tryScope := t.findTryScopeAbove(targetDepth); tryScope != nil {
		flag := "____hasBroken"
		if tsLabel != "" {
			flag = "____hasBroken_" + tsLabel
		}
		tryScope.addDeferredTransfer(DeferredTransfer{
			Kind:     TransferBreak,
			TSLabel:  tsLabel,
			FlagName: flag,
			Dispatch: normal,
		})
		return []lua.Statement{
			lua.Assign([]lua.Expression{lua.Ident(flag)}, []lua.Expression{lua.Bool(true)}),
			lua.Return(),
		}
	}

	return normal
}

// buildContinueDispatch emits the Lua statements for a TS continue statement,
// routing through the sentinel-flag path when the continue would cross a
// pcall/awaiter function boundary from within a try/catch body.
func (t *Transpiler) buildContinueDispatch(tsLabel string) []lua.Statement {
	targetDepth := -1
	if tsLabel != "" {
		if d, ok := t.labelScopeDepths[tsLabel]; ok {
			targetDepth = d - 1
		}
	} else {
		targetDepth = t.findInnermostLoopDepth()
	}

	normal := t.normalContinueStatements(tsLabel)

	if tryScope := t.findTryScopeAbove(targetDepth); tryScope != nil {
		flag := "____hasContinued"
		if tsLabel != "" {
			flag = "____hasContinued_" + tsLabel
		}
		tryScope.addDeferredTransfer(DeferredTransfer{
			Kind:     TransferContinue,
			TSLabel:  tsLabel,
			FlagName: flag,
			Dispatch: normal,
		})
		return []lua.Statement{
			lua.Assign([]lua.Expression{lua.Ident(flag)}, []lua.Expression{lua.Bool(true)}),
			lua.Return(),
		}
	}

	return normal
}

// normalBreakStatements returns the Lua statements for a break (labeled or
// unlabeled) assuming the jump doesn't cross a pcall/awaiter boundary.
func (t *Transpiler) normalBreakStatements(tsLabel string) []lua.Statement {
	if tsLabel != "" {
		if luaLabel, ok := t.breakLabels[tsLabel]; ok {
			if t.luaTarget.SupportsGoto() {
				return []lua.Statement{lua.Goto(luaLabel)}
			}
			return []lua.Statement{
				lua.Assign([]lua.Expression{lua.Ident(luaLabel)}, []lua.Expression{lua.Bool(true)}),
				lua.Break(),
			}
		}
	}
	return []lua.Statement{lua.Break()}
}

// normalContinueStatements returns the Lua statements for a continue (labeled
// or unlabeled) assuming the jump doesn't cross a pcall/awaiter boundary.
func (t *Transpiler) normalContinueStatements(tsLabel string) []lua.Statement {
	if tsLabel != "" {
		if luaLabel, ok := t.continueLabelMap[tsLabel]; ok {
			if t.luaTarget.SupportsGoto() {
				return []lua.Statement{lua.Goto(luaLabel)}
			}
			return []lua.Statement{
				lua.Assign([]lua.Expression{lua.Ident(luaLabel)}, []lua.Expression{lua.Bool(true)}),
				lua.Break(),
			}
		}
	}
	if t.luaTarget.HasNativeContinue() {
		// For C-style for-loops, the incrementor must run before continue.
		// Duplicate it here since native continue skips to the loop condition.
		var stmts []lua.Statement
		if n := len(t.forLoopPreContinue); n > 0 {
			stmts = append(stmts, t.forLoopPreContinue[n-1]...)
		}
		if n := len(t.forLoopIncrementors); n > 0 {
			if inc := t.forLoopIncrementors[n-1]; inc != nil {
				stmts = append(stmts, inc...)
			}
		}
		return append(stmts, lua.Continue())
	}
	label := "__continue"
	if len(t.continueLabels) > 0 {
		label = t.continueLabels[len(t.continueLabels)-1]
	}
	if t.luaTarget.SupportsGoto() {
		return []lua.Statement{lua.Goto(label)}
	}
	return []lua.Statement{
		lua.Assign([]lua.Expression{lua.Ident(label)}, []lua.Expression{lua.Bool(true)}),
		lua.Break(),
	}
}

// isInsideFunction returns true if any scope in the stack is a function scope.
// Used to check if code is inside a function body vs at module/file level.
func (t *Transpiler) isInsideFunction() bool {
	for _, scope := range t.scopeStack {
		if scope.Type == ScopeFunction {
			return true
		}
	}
	return false
}

// isTopLevelScope returns true if the current scope is the file-level scope
// (not inside any function or block). Used to determine global vs local declarations.
func (t *Transpiler) isTopLevelScope() bool {
	if len(t.scopeStack) == 0 {
		return true
	}
	return t.scopeStack[len(t.scopeStack)-1].Type == ScopeFile
}

// isExportAsGlobalTopLevel returns true when exports should be emitted as bare
// globals — i.e. exportAsGlobal is enabled and we're not inside a namespace.
func (t *Transpiler) isExportAsGlobalTopLevel() bool {
	return t.exportAsGlobal && t.currentNamespace == ""
}

// shouldUseLocalDeclaration returns true if a non-exported declaration should use `local`.
// In modules or inside functions/blocks, declarations are always local.
// At file scope in non-module scripts, declarations are global (matching TSTL behavior)
// unless noImplicitGlobalVariables is set.
func (t *Transpiler) shouldUseLocalDeclaration() bool {
	return t.isModule || !t.isTopLevelScope() || t.noImplicitGlobalVariables
}

// inScope returns true if any scope on the stack matches the given type.
func (t *Transpiler) inScope() bool {
	return len(t.scopeStack) > 0
}

// trackSymbolReference records a symbol reference in ALL current scopes.
func (t *Transpiler) trackSymbolReference(sym *ast.Symbol, node *ast.Node) SymbolID {
	if t.symbolIDMap == nil {
		t.symbolIDMap = make(map[*ast.Symbol]SymbolID)
		t.symbolInfoMap = make(map[SymbolID]*SymbolInfo)
	}

	symbolID, exists := t.symbolIDMap[sym]
	if !exists {
		t.lastSymbolID++
		symbolID = t.lastSymbolID
		t.symbolIDMap[sym] = symbolID
		t.symbolInfoMap[symbolID] = &SymbolInfo{
			Symbol:         sym,
			FirstSeenAtPos: node.Pos(),
		}
	} else if info := t.symbolInfoMap[symbolID]; node.Pos() < info.FirstSeenAtPos {
		// Update to earliest reference position (for hoisting decisions)
		info.FirstSeenAtPos = node.Pos()
	}

	// Mark as referenced in all current scopes (increment count)
	for _, scope := range t.scopeStack {
		if scope.ReferencedSymbols == nil {
			scope.ReferencedSymbols = make(map[SymbolID]int)
		}
		scope.ReferencedSymbols[symbolID]++
	}

	return symbolID
}

// markSymbolDeclared registers a symbol as "declared" by incrementing its reference
// count in all current scopes. This counts as the first reference for
// hasMultipleReferences purposes, without affecting FirstSeenAtPos (which tracks
// first usage, not declaration, for hoisting decisions).
func (t *Transpiler) markSymbolDeclared(symbolID SymbolID) {
	for _, scope := range t.scopeStack {
		if scope.ReferencedSymbols == nil {
			scope.ReferencedSymbols = make(map[SymbolID]int)
		}
		scope.ReferencedSymbols[symbolID]++
	}
}

// hasMultipleReferences checks if any of the given symbols have been referenced
// more than once in the given scope. The first reference is typically the declaration
// site; additional references indicate the symbol is used in its own initializer
// (e.g. via a callback closure), requiring declaration splitting.
func (t *Transpiler) hasMultipleReferences(scope *Scope, symbolIDs ...SymbolID) bool {
	if scope.ReferencedSymbols == nil {
		return false
	}
	for _, id := range symbolIDs {
		if id != 0 && scope.ReferencedSymbols[id] > 1 {
			return true
		}
	}
	return false
}

// getSymbolID returns the SymbolID for a Symbol, or 0 if not tracked.
func (t *Transpiler) getSymbolID(sym *ast.Symbol) SymbolID {
	if t.symbolIDMap == nil {
		return 0
	}
	return t.symbolIDMap[sym]
}

// getOrCreateSymbolID returns an existing SymbolID or creates a new one (without recording a reference position).
func (t *Transpiler) getOrCreateSymbolID(sym *ast.Symbol) SymbolID {
	if t.symbolIDMap == nil {
		t.symbolIDMap = make(map[*ast.Symbol]SymbolID)
		t.symbolInfoMap = make(map[SymbolID]*SymbolInfo)
	}
	if id, ok := t.symbolIDMap[sym]; ok {
		return id
	}
	t.lastSymbolID++
	id := t.lastSymbolID
	t.symbolIDMap[sym] = id
	t.symbolInfoMap[id] = &SymbolInfo{
		Symbol:         sym,
		FirstSeenAtPos: int(^uint(0) >> 1), // max int — not yet seen as a reference
	}
	return id
}

// addScopeVariableDeclaration registers a variable declaration in the current scope for hoisting.
func (t *Transpiler) addScopeVariableDeclaration(decl *lua.VariableDeclarationStatement, symbolIDs ...SymbolID) {
	scope := t.peekScope()
	scope.VariableDecls = append(scope.VariableDecls, TrackedVarDecl{
		Stmt:      decl,
		SymbolIDs: symbolIDs,
	})
}

// registerFunctionDefinition registers a function's referenced symbols in the parent scope.
func (t *Transpiler) registerFunctionDefinition(symbolID SymbolID, funcScope *Scope) {
	scope := t.peekScope()
	if scope.FunctionDefs == nil {
		scope.FunctionDefs = make(map[SymbolID]*FunctionDefinitionInfo)
	}
	scope.FunctionDefs[symbolID] = &FunctionDefinitionInfo{
		ReferencedSymbols: funcScope.ReferencedSymbols,
	}
}

// setFunctionDefinitionStatement sets the lua statement for a previously registered function definition.
func (t *Transpiler) setFunctionDefinitionStatement(symbolID SymbolID, stmt lua.Statement) {
	scope := t.peekScope()
	if scope.FunctionDefs != nil {
		if info, ok := scope.FunctionDefs[symbolID]; ok {
			info.Definition = stmt
		}
	}
}

// getFirstDeclarationInFile returns the declaration with the smallest position in the current source file.
func (t *Transpiler) getFirstDeclarationInFile(sym *ast.Symbol) *ast.Node {
	var first *ast.Node
	for _, decl := range sym.Declarations {
		sf := ast.GetSourceFileOfNode(decl)
		if sf == nil || sf != t.sourceFile {
			continue
		}
		if first == nil || decl.Pos() < first.Pos() {
			first = decl
		}
	}
	return first
}

// sortedFuncDefKeys returns FunctionDefs keys sorted by SymbolID.
// SymbolIDs are assigned in AST visit order, so sorting preserves source order
// and ensures deterministic iteration (Go map order is randomized).
func sortedFuncDefKeys(scope *Scope) []SymbolID {
	if len(scope.FunctionDefs) == 0 {
		return nil
	}
	keys := make([]SymbolID, 0, len(scope.FunctionDefs))
	for id := range scope.FunctionDefs {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	return keys
}

// shouldHoistSymbol checks whether a symbol should be hoisted in the given scope.
func (t *Transpiler) shouldHoistSymbol(symbolID SymbolID, scope *Scope) bool {
	// Always hoist in switch scopes
	if scope.Type == ScopeSwitch {
		return true
	}

	info := t.symbolInfoMap[symbolID]
	if info == nil {
		return false
	}

	decl := t.getFirstDeclarationInFile(info.Symbol)
	if decl == nil {
		return false
	}

	// Symbol referenced before its declaration
	if info.FirstSeenAtPos < decl.Pos() {
		return true
	}

	// Symbol referenced from a hoisted function that is defined after the declaration
	for _, funcSymID := range sortedFuncDefKeys(scope) {
		funcDef := scope.FunctionDefs[funcSymID]
		if funcSymID == symbolID {
			continue // don't recurse into self
		}
		if funcDef.ReferencedSymbols != nil && funcDef.ReferencedSymbols[symbolID] > 0 {
			// Check if the function is itself hoisted (i.e., referenced before its definition)
			if t.shouldHoistSymbol(funcSymID, scope) {
				return true
			}
		}
	}

	return false
}

// performHoisting rearranges transformed lua statements based on scope-tracked symbol info.
// It moves hoisted function definitions and forward-declares hoisted variables.
func (t *Transpiler) performHoisting(scope *Scope, stmts []lua.Statement) []lua.Statement {
	hoistedStmts, hoistedIdents, remaining := t.separateHoistedStatements(scope, stmts)

	// Hoist import statements to the very top
	importStmts, remaining := t.hoistImportStatements(scope, remaining)

	result := make([]lua.Statement, 0, len(importStmts)+len(hoistedIdents)+len(hoistedStmts)+len(remaining))
	result = append(result, importStmts...)
	if len(hoistedIdents) > 0 {
		result = append(result, lua.LocalDecl(hoistedIdents, nil))
	}
	result = append(result, hoistedStmts...)
	result = append(result, remaining...)
	return result
}

// registerImportStatements registers import statements in the current scope for hoisting.
func (t *Transpiler) registerImportStatements(stmts []lua.Statement) {
	if !t.inScope() {
		return
	}
	scope := t.peekScope()
	scope.ImportStatements = append(scope.ImportStatements, stmts...)
}

// hoistImportStatements removes import statements from their inline position
// and returns them separately to be prepended at the scope top.
func (t *Transpiler) hoistImportStatements(scope *Scope, stmts []lua.Statement) ([]lua.Statement, []lua.Statement) {
	if len(scope.ImportStatements) == 0 {
		return nil, stmts
	}
	// Build a set of import statement pointers for fast lookup
	importSet := make(map[lua.Statement]bool, len(scope.ImportStatements))
	for _, s := range scope.ImportStatements {
		importSet[s] = true
	}
	// Remove import statements from their inline positions
	remaining := make([]lua.Statement, 0, len(stmts))
	for _, s := range stmts {
		if !importSet[s] {
			remaining = append(remaining, s)
		}
	}
	return scope.ImportStatements, remaining
}

// separateHoistedStatements splits statements into hoisted and non-hoisted parts.
// Returns: (hoistedStatements, hoistedIdentifiers, remainingStatements)
func (t *Transpiler) separateHoistedStatements(scope *Scope, stmts []lua.Statement) ([]lua.Statement, []*lua.Identifier, []lua.Statement) {
	var hoistedStmts []lua.Statement
	var hoistedIdents []*lua.Identifier

	remaining := make([]lua.Statement, len(stmts))
	copy(remaining, stmts)

	// Hoist function definitions (sorted by SymbolID for deterministic output)
	if scope.FunctionDefs != nil {
		for _, funcSymID := range sortedFuncDefKeys(scope) {
			funcDef := scope.FunctionDefs[funcSymID]
			if funcDef.Definition == nil {
				continue
			}
			if !t.shouldHoistSymbol(funcSymID, scope) {
				continue
			}
			// Find and remove from remaining statements
			idx := -1
			for i, s := range remaining {
				if s == funcDef.Definition {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			remaining = append(remaining[:idx], remaining[idx+1:]...)

			// If it's a local declaration, separate into identifier + assignment
			if varDecl, ok := funcDef.Definition.(*lua.VariableDeclarationStatement); ok && varDecl.Right != nil {
				hoistedIdents = append(hoistedIdents, varDecl.Left...)
				hoistedStmts = append(hoistedStmts, lua.Assign(
					identExprs(varDecl.Left),
					varDecl.Right,
				))
			} else {
				hoistedStmts = append(hoistedStmts, funcDef.Definition)
			}
		}
	}

	// Hoist variable declarations
	for _, tracked := range scope.VariableDecls {
		shouldHoist := false
		// Check using tracked SymbolIDs (precise) or fall back to name lookup
		if len(tracked.SymbolIDs) > 0 {
			for _, symID := range tracked.SymbolIDs {
				if symID != 0 && t.shouldHoistSymbol(symID, scope) {
					shouldHoist = true
					break
				}
			}
		} else {
			// Fallback: resolve by identifier name
			for _, ident := range tracked.Stmt.Left {
				sym := t.resolveIdentSymbol(ident.Text)
				if sym != nil {
					symID := t.getSymbolID(sym)
					if symID != 0 && t.shouldHoistSymbol(symID, scope) {
						shouldHoist = true
						break
					}
				}
			}
		}
		if !shouldHoist {
			continue
		}
		// Find and modify in remaining statements
		idx := -1
		for i, s := range remaining {
			if s == tracked.Stmt {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		if tracked.Stmt.Right != nil {
			// Replace local x = val with x = val
			remaining[idx] = lua.Assign(identExprs(tracked.Stmt.Left), tracked.Stmt.Right)
		} else {
			// Remove empty declaration
			remaining = append(remaining[:idx], remaining[idx+1:]...)
		}
		hoistedIdents = append(hoistedIdents, tracked.Stmt.Left...)
	}

	return hoistedStmts, hoistedIdents, remaining
}

// resolveIdentSymbol finds the checker.Symbol for a lua identifier name by searching declarations.
func (t *Transpiler) resolveIdentSymbol(name string) *ast.Symbol {
	if t.symbolIDMap == nil {
		return nil
	}
	for sym := range t.symbolIDMap {
		if ast.SymbolName(sym) == name {
			return sym
		}
	}
	return nil
}

// isLocalShadow returns true if an identifier refers to a local variable/parameter
// that shadows a module-level export. This prevents local `foo` parameters from
// being rewritten to `____exports.foo`.
func (t *Transpiler) isLocalShadow(node *ast.Node) bool {

	sym := t.checker.GetSymbolAtLocation(node)
	if sym == nil {
		return false
	}
	// Check if the symbol is declared as a parameter or in a local scope
	for _, decl := range sym.Declarations {
		switch decl.Kind {
		case ast.KindParameter:
			return true
		case ast.KindVariableDeclaration:
			// Check if the declaration is inside a function, block, or other local scope
			// (not at the module/script top level).
			for p := decl.Parent; p != nil; p = p.Parent {
				switch p.Kind {
				case ast.KindFunctionDeclaration, ast.KindFunctionExpression,
					ast.KindArrowFunction, ast.KindMethodDeclaration,
					ast.KindConstructor:
					return true
				case ast.KindBlock:
					// A Block whose parent is a function body is handled above.
					// A standalone block (e.g., { let a = 2 }) at top level is still a local scope.
					if p.Parent != nil && p.Parent.Kind != ast.KindSourceFile {
						return true
					}
					// Top-level block: check if it's a VariableStatement (var) at source level
					// vs a standalone block. Standalone blocks create local scopes for let/const.
					if decl.Parent != nil && decl.Parent.Flags&ast.NodeFlagsConst != 0 || decl.Parent.Flags&ast.NodeFlagsLet != 0 {
						return true
					}
				}
			}
		}
	}
	return false
}

// isAmbientSymbol returns true if the node's symbol is an ambient declaration (declare keyword or .d.ts).
func (t *Transpiler) isAmbientSymbol(node *ast.Node) bool {
	sym := t.checker.GetSymbolAtLocation(node)
	if sym == nil {
		return false
	}
	for _, decl := range sym.Declarations {
		if ast.GetCombinedModifierFlags(decl)&ast.ModifierFlagsAmbient != 0 {
			return true
		}
	}
	return false
}

// getIdentifierExportScope returns the Lua expression for the scope that exports this identifier,
// or nil if the identifier is not exported. Handles both namespace exports and module exports.
func (t *Transpiler) getIdentifierExportScope(node *ast.Node) lua.Expression {

	sym := t.checker.GetSymbolAtLocation(node)
	if sym == nil {
		return nil
	}
	// Check if any declaration of this symbol is an exported member of a namespace or module
	for _, decl := range sym.Declarations {
		// Check for export modifier on the declaration or its parent statement
		isExport := hasExportModifier(decl)
		if !isExport && decl.Parent != nil {
			// For variable declarations, the export is on the parent VariableStatement
			if decl.Kind == ast.KindVariableDeclaration && decl.Parent.Parent != nil {
				isExport = hasExportModifier(decl.Parent.Parent)
			}
		}
		if !isExport {
			continue
		}
		// Walk up to find the containing module declaration (for namespace exports)
		var moduleDecl *ast.Node
		for p := decl.Parent; p != nil; p = p.Parent {
			if p.Kind == ast.KindModuleBlock && p.Parent != nil && p.Parent.Kind == ast.KindModuleDeclaration {
				moduleDecl = p.Parent
				break
			}
		}
		if moduleDecl != nil {
			return t.createModuleLocalName(moduleDecl)
		}
		// Module-level export: return ____exports
		if t.isModule {
			return lua.Ident("____exports")
		}
	}
	return nil
}

// identExprs converts a slice of *lua.Identifier to []lua.Expression.
func identExprs(idents []*lua.Identifier) []lua.Expression {
	exprs := make([]lua.Expression, len(idents))
	for i, id := range idents {
		exprs[i] = id
	}
	return exprs
}

func (t *Transpiler) transformExpression(node *ast.Node) (result lua.Expression) {
	// Set source position on the result expression for source map generation.
	// Only sets position if the transform function didn't already set one (fallback).
	if t.sourceMapEnabled {
		defer func() {
			if result != nil {
				if n, ok := result.(lua.Node); ok && !n.SourcePosition().HasPos {
					if p, ok := result.(lua.Positioned); ok {
						t.setNodePos(p, node)
					}
				}
			}
		}()
	}

	switch node.Kind {
	case ast.KindNumericLiteral:
		text := node.AsNumericLiteral().Text
		// Numbers overflowing float64 become math.huge (matches TSTL behavior)
		val, err := strconv.ParseFloat(text, 64)
		if err == nil && math.IsInf(val, 1) {
			if t.luaTarget.HasMathHuge() {
				return memberAccess(lua.Ident("math"), "huge")
			}
			return lua.Binary(lua.Num("1"), lua.OpDiv, lua.Num("0"))
		}
		if err == nil && math.IsInf(val, -1) {
			if t.luaTarget.HasMathHuge() {
				return lua.Unary(lua.OpNeg, memberAccess(lua.Ident("math"), "huge"))
			}
			return lua.Unary(lua.OpNeg, lua.Binary(lua.Num("1"), lua.OpDiv, lua.Num("0")))
		}
		return lua.Num(text)
	case ast.KindStringLiteral:
		return lua.Str(node.AsStringLiteral().Text)
	case ast.KindNoSubstitutionTemplateLiteral:
		return lua.Str(node.AsNoSubstitutionTemplateLiteral().Text)
	case ast.KindTrueKeyword:
		return lua.Bool(true)
	case ast.KindFalseKeyword:
		return lua.Bool(false)
	case ast.KindNullKeyword, ast.KindUndefinedKeyword:
		return lua.Nil()
	case ast.KindIdentifier:
		// Track symbol reference for hoisting analysis
		if t.inScope() {
			if sym := t.checker.GetSymbolAtLocation(node); sym != nil {
				t.trackSymbolReference(sym, node)
			}
		}
		text := node.AsIdentifier().Text
		switch text {
		case "undefined":
			return lua.Nil()
		case "Infinity":
			if !t.hasLocalBinding(node) {
				if t.luaTarget.HasMathHuge() {
					return lua.Index(lua.Ident("math"), lua.Str("huge"))
				}
				return lua.Binary(lua.Num("1"), lua.OpDiv, lua.Num("0"))
			}
		case "NaN":
			if !t.hasLocalBinding(node) {
				return lua.Binary(lua.Num("0"), lua.OpDiv, lua.Num("0"))
			}
		case "globalThis":
			return lua.Ident("_G")
		case "Promise":
			fn := t.requireLualib("__TS__Promise")
			return lua.Ident(fn)
		case "Symbol":
			t.requireLualib("__TS__Symbol")
			t.requireLualib("Symbol")
			return lua.Ident("Symbol")
		case "Map", "Set", "WeakMap", "WeakSet":
			fn := t.requireLualib(text)
			return lua.Ident(fn)
		case "Error", "RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError":
			fn := t.requireLualib(text)
			return lua.Ident(fn)
		}
		if result := t.checkExtensionIdentifier(node, text); result != nil {
			return result
		}
		originalText := text
		text = t.resolveIdentifierName(node, text)
		// Check @customName annotation on the symbol's declaration
		if sym := t.checker.GetSymbolAtLocation(node); sym != nil {
			if customName := t.getCustomNameFromSymbol(sym); customName != "" {
				text = customName
			}
			if renamed, ok := t.loopVarRenames[sym]; ok {
				text = renamed
			}
		}
		// Check namespace export scope before module export
		if t.currentNamespace != "" {
			if nsTarget := t.getIdentifierExportScope(node); nsTarget != nil {
				return lua.Index(nsTarget, lua.Str(node.AsIdentifier().Text))
			}
		}
		if t.isModule && !t.exportAsGlobal && t.exportedNames[node.AsIdentifier().Text] {
			// Only rewrite to ____exports if the symbol actually refers to the module-level export,
			// not a local shadow (e.g., parameter or local variable with the same name)
			if !t.isLocalShadow(node) {
				return lua.Index(lua.Ident("____exports"), lua.Str(node.AsIdentifier().Text))
			}
		}
		ident := lua.Ident(text)
		if text != originalText {
			t.setNodePosNamed(ident, node, originalText)
		}
		return ident
	case ast.KindBinaryExpression:
		return t.transformBinaryExpression(node)
	case ast.KindCallExpression:
		result := t.transformCallExpression(node)
		// Multi-return calls in single-value contexts must be wrapped in a table
		// to capture all return values: {call()} → table with all returns.
		if t.shouldMultiReturnCallBeWrapped(node) {
			return lua.Table(lua.Field(result))
		}
		return result
	case ast.KindPropertyAccessExpression:
		return t.transformPropertyAccessExpression(node)
	case ast.KindElementAccessExpression:
		return t.transformElementAccessExpression(node)
	case ast.KindParenthesizedExpression:
		// TS-level parens are for grouping/precedence which the Lua printer handles.
		// Don't emit Lua-level parens — they're only needed for multi-return truncation
		// or IIFE wrapping, both handled by the Lua printer itself.
		inner := node.AsParenthesizedExpression().Expression
		return t.transformExpression(inner)
	case ast.KindPrefixUnaryExpression:
		return t.transformPrefixUnaryExpression(node)
	case ast.KindPostfixUnaryExpression:
		return t.transformPostfixUnaryExpression(node)
	case ast.KindArrayLiteralExpression:
		return t.transformArrayLiteral(node)
	case ast.KindObjectLiteralExpression:
		return t.transformObjectLiteral(node)
	case ast.KindTemplateExpression:
		return t.transformTemplateExpression(node)
	case ast.KindConditionalExpression:
		return t.transformConditionalExpression(node)
	case ast.KindTypeOfExpression:
		return t.transformTypeOfExpression(node)
	case ast.KindSpreadElement:
		return t.transformSpreadElement(node)
	case ast.KindThisKeyword:
		selfIdent := lua.Ident("self")
		t.setNodePosNamed(selfIdent, node, "this")
		return selfIdent
	case ast.KindNewExpression:
		return t.transformNewExpression(node)
	case ast.KindArrowFunction:
		return t.transformArrowFunction(node)
	case ast.KindFunctionExpression:
		return t.transformFunctionExpression(node)
	case ast.KindAsExpression:
		t.validateTypeAssertion(node)
		return t.transformExpression(node.AsAsExpression().Expression)
	case ast.KindTypeAssertionExpression:
		t.validateTypeAssertion(node)
		return t.transformExpression(node.AsTypeAssertion().Expression)
	case ast.KindNonNullExpression:
		return t.transformExpression(node.AsNonNullExpression().Expression)
	case ast.KindSatisfiesExpression:
		return t.transformExpression(node.AsSatisfiesExpression().Expression)
	case ast.KindVoidExpression:
		return t.transformVoidExpression(node)
	case ast.KindComputedPropertyName:
		return t.transformExpression(node.AsComputedPropertyName().Expression)
	case ast.KindSuperKeyword:
		return lua.Ident("self")
	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern:
		return lua.Ident(t.transformBindingPatternParam(node))
	case ast.KindDeleteExpression:
		return t.transformDeleteExpression(node)
	case ast.KindTaggedTemplateExpression:
		return t.transformTaggedTemplateExpression(node)
	case ast.KindClassExpression:
		return t.transformClassExpression(node)
	case ast.KindAwaitExpression:
		if t.asyncDepth == 0 {
			t.addError(node, dw.AwaitMustBeInAsyncFunction, "Await can only be used inside async functions.")
		}
		ae := node.AsAwaitExpression()
		fn := t.requireLualib("__TS__Await")
		return lua.Call(lua.Ident(fn), t.transformExpression(ae.Expression))
	case ast.KindYieldExpression:
		return t.transformYieldExpression(node)
	case ast.KindOmittedExpression:
		return lua.Nil()
	case ast.KindExpressionWithTypeArguments:
		// TS 4.7 instantiation expressions: foo<number> → just foo (type args erased)
		ewta := node.AsExpressionWithTypeArguments()
		return t.transformExpression(ewta.Expression)
	case ast.KindQualifiedName:
		qn := node.AsQualifiedName()
		left := t.transformExpression(qn.Left)
		return lua.Index(left, lua.Str(qn.Right.AsIdentifier().Text))
	case ast.KindExternalModuleReference:
		emr := node.AsExternalModuleReference()
		modulePath := t.resolveModulePath(emr.Expression)
		return lua.Call(lua.Ident("require"), lua.Str(modulePath))
	case ast.KindJsxElement:
		if t.validateJsxConfig(node) {
			return t.transformJsxElement(node)
		}
		t.addError(node, dw.UnsupportedNodeKind, fmt.Sprintf("unsupported expression: %s", node.Kind.String()))
		return lua.Comment(fmt.Sprintf("nil --[[ unsupported: %s ]]", node.Kind.String()))
	case ast.KindJsxSelfClosingElement:
		if t.validateJsxConfig(node) {
			return t.transformJsxSelfClosingElement(node)
		}
		t.addError(node, dw.UnsupportedNodeKind, fmt.Sprintf("unsupported expression: %s", node.Kind.String()))
		return lua.Comment(fmt.Sprintf("nil --[[ unsupported: %s ]]", node.Kind.String()))
	case ast.KindJsxFragment:
		if t.validateJsxConfig(node) {
			return t.transformJsxFragment(node)
		}
		t.addError(node, dw.UnsupportedNodeKind, fmt.Sprintf("unsupported expression: %s", node.Kind.String()))
		return lua.Comment(fmt.Sprintf("nil --[[ unsupported: %s ]]", node.Kind.String()))
	case ast.KindJsxExpression:
		expr := node.AsJsxExpression()
		if expr.Expression == nil {
			return lua.Ident("nil")
		}
		return t.transformExpression(expr.Expression)
	case ast.KindBigIntLiteral,
		ast.KindRegularExpressionLiteral:
		t.addError(node, dw.UnsupportedNodeKind, fmt.Sprintf("unsupported expression: %s", node.Kind.String()))
		return lua.Comment(fmt.Sprintf("nil --[[ unsupported: %s ]]", node.Kind.String()))
	default:
		return lua.Comment(fmt.Sprintf("nil --[[ TODO: unhandled expression: %s (%d) ]]", node.Kind.String(), node.Kind))
	}
}

// collectExportedNames does a pre-pass to identify exported declarations.
// This allows identifier references to exported names to be rewritten as ____exports.name.
func (t *Transpiler) collectExportedNames(sf *ast.SourceFile) {
	t.exportedNames = make(map[string]bool)
	t.namedExports = make(map[string][]string)
	for _, stmt := range sf.Statements.Nodes {
		// Handle `export { foo, bar }` and `export { x as a }` named export declarations
		if stmt.Kind == ast.KindExportDeclaration {
			ed := stmt.AsExportDeclaration()
			if !ed.IsTypeOnly && ed.ExportClause != nil && ed.ModuleSpecifier == nil {
				if ed.ExportClause.Kind == ast.KindNamedExports {
					for _, spec := range ed.ExportClause.AsNamedExports().Elements.Nodes {
						es := spec.AsExportSpecifier()
						// Skip type-only export specifiers
						if es.IsTypeOnly {
							continue
						}
						// Skip exports of type-only symbols (interfaces, type aliases)
						if sym := t.checker.GetSymbolAtLocation(es.Name()); sym != nil {
							resolved := sym
							if sym.Flags&ast.SymbolFlagsAlias != 0 {
								resolved = checker.Checker_getImmediateAliasedSymbol(t.checker, sym)
							}
							if resolved != nil && resolved.Flags&ast.SymbolFlagsValue == 0 {
								continue
							}
						}
						exportName := es.Name().AsIdentifier().Text
						localName := exportName
						if es.PropertyName != nil {
							localName = es.PropertyName.AsIdentifier().Text
						}
						t.namedExports[localName] = append(t.namedExports[localName], exportName)
					}
				}
			}
			continue
		}
		if !hasExportModifier(stmt) {
			continue
		}
		switch stmt.Kind {
		case ast.KindVariableStatement:
			vs := stmt.AsVariableStatement()
			for _, decl := range vs.DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
				d := decl.AsVariableDeclaration()
				if d.Name().Kind == ast.KindIdentifier {
					t.exportedNames[d.Name().AsIdentifier().Text] = true
				}
			}
		case ast.KindFunctionDeclaration:
			fd := stmt.AsFunctionDeclaration()
			if fd.Name() != nil && !hasDefaultModifier(stmt) {
				t.exportedNames[fd.Name().AsIdentifier().Text] = true
			}
		case ast.KindClassDeclaration:
			cd := stmt.AsClassDeclaration()
			if cd.Name() != nil && !hasDefaultModifier(stmt) {
				t.exportedNames[cd.Name().AsIdentifier().Text] = true
			}
		case ast.KindEnumDeclaration:
			// Const enums are inlined and have no runtime representation — don't export.
			if ast.GetCombinedModifierFlags(stmt)&ast.ModifierFlagsConst == 0 {
				ed := stmt.AsEnumDeclaration()
				t.exportedNames[ed.Name().AsIdentifier().Text] = true
			}
		}
	}
}

// ==========================================================================
// JSDoc comment helpers
// ==========================================================================

// getLeadingComments returns Lua comment strings for a declaration node's leading comments.
// Respects the removeComments tsconfig option. When comments are preserved:
//   - Single-line comments (// ...) become "-- ..."
//   - Block comments (/* ... */) become "--[[ ... ]]"
//   - JSDoc comments (/** ... */) become LDoc: "--- ..." / "-- ..."
func (t *Transpiler) getLeadingComments(node *ast.Node) []string {
	if t.removeComments {
		return nil
	}

	sourceText := t.sourceFile.Text()
	f := &ast.NodeFactory{}
	var comments []string

	for cr := range scanner.GetLeadingCommentRanges(f, sourceText, node.Pos()) {
		comments = append(comments, formatCommentRange(sourceText, cr)...)
	}

	if len(comments) == 0 {
		return nil
	}
	return comments
}

// getTrailingComments returns Lua comment strings for a statement's trailing
// comments (same-line comments after the statement's end position).
func (t *Transpiler) getTrailingComments(node *ast.Node) []string {
	if t.removeComments {
		return nil
	}

	sourceText := t.sourceFile.Text()
	f := &ast.NodeFactory{}
	var comments []string

	for cr := range scanner.GetTrailingCommentRanges(f, sourceText, node.End()) {
		comments = append(comments, formatCommentRange(sourceText, cr)...)
	}

	if len(comments) == 0 {
		return nil
	}
	return comments
}

// formatCommentRange converts a single TS comment range into Lua comment lines.
//
//   - Single-line (// ...) → "-- ..."
//   - Block (/* ... */) → "-- ..." (single line) or "--[[ ... ]]" (multi-line)
//   - JSDoc (/** ... */) → LDoc: "--- first" / "-- rest" (with "---" header when first line is an @tag)
func formatCommentRange(sourceText string, cr ast.CommentRange) []string {
	raw := sourceText[cr.Pos():cr.End()]
	switch cr.Kind {
	case ast.KindSingleLineCommentTrivia:
		text := strings.TrimPrefix(raw, "//")
		if len(text) > 0 && text[0] == ' ' {
			text = text[1:]
		}
		return []string{"-- " + text}

	case ast.KindMultiLineCommentTrivia:
		if strings.HasPrefix(raw, "/**") {
			lines := parseJSDocText(raw)
			if len(lines) == 0 {
				return nil
			}
			var out []string
			for i, line := range lines {
				if i == 0 {
					if strings.HasPrefix(line, "@") {
						out = append(out, "---", "-- "+line)
					} else {
						out = append(out, "--- "+line)
					}
				} else {
					out = append(out, "-- "+line)
				}
			}
			return out
		}
		inner := strings.TrimPrefix(raw, "/*")
		inner = strings.TrimSuffix(inner, "*/")
		inner = strings.TrimSpace(inner)
		if strings.Contains(inner, "\n") {
			return []string{"--[[ " + inner + " ]]"}
		}
		return []string{"-- " + inner}
	}
	return nil
}

func (t *Transpiler) shouldInjectNoSelfInFileDeclaration() bool {
	if t.sourceFile == nil {
		return false
	}
	if hasFileAnnotation(t.sourceFile, AnnotNoSelfInFile) {
		return true
	}
	if !t.noImplicitSelf || t.program == nil {
		return false
	}
	return !compiler.Program_IsSourceFileDefaultLibrary(t.program, t.sourceFile.Path()) &&
		!compiler.Program_IsSourceFileFromExternalLibrary(t.program, t.sourceFile)
}

func (t *Transpiler) postProcessDeclaration(text string) string {
	if text == "" {
		return ""
	}
	if t.shouldInjectNoSelfInFileDeclaration() && !strings.Contains(text, "@noSelfInFile") {
		return "/** @noSelfInFile */\n" + text
	}
	return text
}

// parseJSDocText extracts clean text lines from a raw JSDoc comment block.
// Input: "/** comment text\n * more text\n * @param x desc\n */"
// Output: ["comment text", "more text", "", "@param x desc"]
func parseJSDocText(raw string) []string {
	// Strip leading /** and trailing */
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "/**")
	s = strings.TrimSuffix(s, "*/")

	rawLines := strings.Split(s, "\n")
	var lines []string
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		// Strip leading * (common in multi-line JSDoc)
		if strings.HasPrefix(line, "* ") {
			line = line[2:]
		} else if line == "*" {
			line = ""
		} else if strings.HasPrefix(line, "*") {
			line = line[1:]
		}
		lines = append(lines, line)
	}

	// Trim leading and trailing empty lines
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// memberAccess builds a chain of table index expressions: base["k1"]["k2"]...
func memberAccess(base lua.Expression, keys ...string) lua.Expression {
	for _, k := range keys {
		base = lua.Index(base, lua.Str(k))
	}
	return base
}

// ==========================================================================
// Statement helpers
// ==========================================================================

// setLeadingComments attaches leading comments to a statement node.
func setLeadingComments(stmt lua.Statement, comments []string) {
	type commentable interface {
		GetComments() *lua.Comments
	}
	if cs, ok := stmt.(commentable); ok {
		cs.GetComments().LeadingComments = comments
	}
}

// setTrailingComments attaches trailing comments to a statement node.
func setTrailingComments(stmt lua.Statement, comments []string) {
	type commentable interface {
		GetComments() *lua.Comments
	}
	if cs, ok := stmt.(commentable); ok {
		cs.GetComments().TrailingComments = comments
	}
}

// transformStatementWithComments transforms a single TS statement and attaches
// its source-text leading and trailing comments to the first and last emitted
// Lua statement. This is the canonical entry point - every caller that lowers
// a TS statement into Lua statements should route through here so comments are
// preserved uniformly regardless of the specific statement kind.
func (t *Transpiler) transformStatementWithComments(node *ast.Node) []lua.Statement {
	stmts := t.transformStatement(node)
	if t.removeComments || len(stmts) == 0 {
		return stmts
	}
	if leading := t.getLeadingComments(node); len(leading) > 0 {
		setLeadingComments(stmts[0], leading)
	}
	if trailing := t.getTrailingComments(node); len(trailing) > 0 {
		setTrailingComments(stmts[len(stmts)-1], trailing)
	}
	return stmts
}

// identList converts a slice of name strings to lua.Identifier nodes.
func identList(names []string) []*lua.Identifier {
	ids := make([]*lua.Identifier, len(names))
	for i, n := range names {
		ids[i] = lua.Ident(n)
	}
	return ids
}

// ==========================================================================
// Type helpers
// ==========================================================================

func (t *Transpiler) isStringType(typ *checker.Type) bool {
	if typ == nil {
		return false
	}
	flags := checker.Type_flags(typ)
	if flags&checker.TypeFlagsStringLike != 0 {
		return true
	}
	// Check unions: string | null | undefined is still string-like
	if flags&checker.TypeFlagsUnion != 0 {
		allStringOrNullish := true
		for _, member := range typ.Types() {
			mf := checker.Type_flags(member)
			if mf&(checker.TypeFlagsStringLike|checker.TypeFlagsUndefined|checker.TypeFlagsNull) == 0 {
				allStringOrNullish = false
				break
			}
		}
		return allStringOrNullish
	}
	// Generic type parameter, indexed access, substitution: check base constraint
	if flags&(checker.TypeFlagsTypeParameter|checker.TypeFlagsIndexedAccess|checker.TypeFlagsSubstitution) != 0 ||
		checker.Type_symbol(typ) != nil {
		base := checker.Checker_getBaseConstraintOfType(t.checker, typ)
		if base != nil && base != typ {
			return t.isStringType(base)
		}
	}
	return false
}

// isElementTypeStringOrNumber checks whether an array's element type is always
// string-like or number-like. Used to optimize .join() to table.concat().
// Ported from: TSTL builtins/array.ts (lines 162-166)
func (t *Transpiler) isElementTypeStringOrNumber(arrayNode *ast.Node) bool {
	if t.checker == nil || arrayNode == nil {
		return false
	}
	typ := t.checker.GetTypeAtLocation(arrayNode)
	if typ == nil {
		return false
	}
	elemType := checker.Checker_getElementTypeOfArrayType(t.checker, typ)
	if elemType == nil {
		return false
	}
	return t.typeAlwaysHasFlags(elemType, checker.TypeFlagsStringLike|checker.TypeFlagsNumberLike)
}

// typeAlwaysHasFlags checks if a type (including unions/intersections) always
// has at least one of the given flags.
// Ported from: TSTL utils/typescript/types.ts typeAlwaysHasSomeOfFlags
func (t *Transpiler) typeAlwaysHasFlags(typ *checker.Type, flags checker.TypeFlags) bool {
	base := checker.Checker_getBaseConstraintOfType(t.checker, typ)
	if base != nil {
		typ = base
	}
	if checker.Type_flags(typ)&flags != 0 {
		return true
	}
	if checker.Type_flags(typ)&checker.TypeFlagsUnion != 0 {
		for _, member := range typ.Types() {
			if !t.typeAlwaysHasFlags(member, flags) {
				return false
			}
		}
		return len(typ.Types()) > 0
	}
	if checker.Type_flags(typ)&checker.TypeFlagsIntersection != 0 {
		for _, member := range typ.Types() {
			if t.typeAlwaysHasFlags(member, flags) {
				return true
			}
		}
	}
	return false
}

// isStandardLibraryType checks whether the type at a node is a standard library type
// with the given symbol name. Used for type-directed builtin detection.
// Ported from: TSTL src/transformation/utils/typescript/index.ts isStandardLibraryType
func (t *Transpiler) isStandardLibraryType(node *ast.Node, name string) bool {
	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return false
	}
	sym := typ.Symbol()
	if sym == nil || sym.Name != name {
		return false
	}
	// If no valueDeclaration, assume it's a lib type (ambient with no source)
	if sym.ValueDeclaration == nil {
		return true
	}
	sf := ast.GetSourceFileOfNode(sym.ValueDeclaration)
	if sf == nil {
		return false
	}
	return compiler.Program_IsSourceFileDefaultLibrary(t.program, sf.Path())
}

func (t *Transpiler) isArrayType(node *ast.Node) bool {

	// Unwrap NonNullExpression (x!) to check the inner expression's type
	expr := node
	for expr.Kind == ast.KindNonNullExpression {
		expr = expr.AsNonNullExpression().Expression
	}
	typ := t.checker.GetTypeAtLocation(expr)
	if typ == nil {
		return false
	}
	// Strip null/undefined from union types (e.g. T | undefined → T)
	typ = checker.Checker_GetNonNullableType(t.checker, typ)
	return t.isArrayTypeFromType(typ)
}

func (t *Transpiler) isArrayTypeFromType(typ *checker.Type) bool {
	flags := checker.Type_flags(typ)
	if flags&checker.TypeFlagsObject != 0 {
		if checker.Checker_isArrayOrTupleType(t.checker, typ) {
			return true
		}
		// Check base types (e.g. interface Foo extends Array<number>)
		// For Interface types, getBaseTypes works directly.
		// For Reference types (instantiated generics like CustomArray<number>),
		// getBaseTypes may panic, so we use hasArrayBaseType with recover.
		objFlags := checker.Type_objectFlags(typ)
		if objFlags&(checker.ObjectFlagsInterface|checker.ObjectFlagsReference) != 0 {
			if t.hasArrayBaseType(typ) {
				return true
			}
		}
		return false
	}
	// Generic type parameter: check base constraint
	if flags&checker.TypeFlagsTypeParameter != 0 {
		base := checker.Checker_getBaseConstraintOfType(t.checker, typ)
		if base != nil {
			return t.isArrayTypeFromType(base)
		}
	}
	// Intersection types: check each member
	if flags&checker.TypeFlagsIntersection != 0 {
		for _, member := range typ.Types() {
			if t.isArrayTypeFromType(member) {
				return true
			}
		}
	}
	// Union types: all non-null/undefined members must be arrays
	if flags&checker.TypeFlagsUnion != 0 {
		hasArray := false
		for _, member := range typ.Types() {
			mf := checker.Type_flags(member)
			if mf&(checker.TypeFlagsNull|checker.TypeFlagsUndefined) != 0 {
				continue
			}
			if !t.isArrayTypeFromType(member) {
				return false
			}
			hasArray = true
		}
		return hasArray
	}
	// Indexed access types (e.g. Record<K, T[]>[K]), substitution types, etc.:
	// resolve via base constraint (matching TSTL's isExplicitArrayType).
	if flags&(checker.TypeFlagsIndexedAccess|checker.TypeFlagsSubstitution|checker.TypeFlagsTypeParameter) != 0 ||
		checker.Type_symbol(typ) != nil {
		base := checker.Checker_getBaseConstraintOfType(t.checker, typ)
		if base != nil && base != typ {
			return t.isArrayTypeFromType(base)
		}
	}
	return false
}

// hasArrayBaseType checks if a type has Array in its base type chain.
// For Reference types (instantiated generics), checks the uninstantiated target
// since getBaseTypes panics on Reference types directly.
func (t *Transpiler) hasArrayBaseType(typ *checker.Type) bool {
	checkType := typ
	// For Reference types, get the uninstantiated target type (the interface definition)
	// because getBaseTypes requires an InterfaceType, not a TypeReference.
	if checker.Type_objectFlags(typ)&checker.ObjectFlagsReference != 0 {
		checkType = typ.Target()
		if checkType == nil {
			return false
		}
	}
	for _, base := range checker.Checker_getBaseTypes(t.checker, checkType) {
		if base != nil && t.isArrayTypeFromType(base) {
			return true
		}
	}
	return false
}

func (t *Transpiler) isMapOrSetType(node *ast.Node) bool {
	return t.isMapType(node) || t.isSetType(node)
}

// mapOrSetTypeName returns the symbol name of a Map/Set type, or "" if not one.
func (t *Transpiler) mapOrSetTypeName(node *ast.Node) string {

	expr := node
	for expr.Kind == ast.KindNonNullExpression {
		expr = expr.AsNonNullExpression().Expression
	}
	typ := t.checker.GetTypeAtLocation(expr)
	if typ == nil {
		return ""
	}
	typ = checker.Checker_GetNonNullableType(t.checker, typ)
	sym := checker.Type_symbol(typ)
	if sym == nil {
		return ""
	}
	return ast.SymbolName(sym)
}

func (t *Transpiler) isMapType(node *ast.Node) bool {
	name := t.mapOrSetTypeName(node)
	return name == "Map" || name == "WeakMap"
}

func (t *Transpiler) isSetType(node *ast.Node) bool {
	name := t.mapOrSetTypeName(node)
	return name == "Set" || name == "WeakSet"
}

func (t *Transpiler) isNumericExpression(node *ast.Node) bool {

	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return false
	}
	flags := checker.Type_flags(typ)
	return flags&checker.TypeFlagsNumberLike != 0
}

func (t *Transpiler) isFunctionType(node *ast.Node) bool {

	// Check if the expression resolves to a function declaration/expression
	sym := t.checker.GetSymbolAtLocation(node)
	if sym != nil {
		for _, decl := range sym.Declarations {
			switch decl.Kind {
			case ast.KindFunctionDeclaration, ast.KindFunctionExpression,
				ast.KindArrowFunction, ast.KindMethodDeclaration:
				return true
			case ast.KindVariableDeclaration:
				// Check if the variable is typed as a function
				vd := decl.AsVariableDeclaration()
				if vd.Initializer != nil {
					switch vd.Initializer.Kind {
					case ast.KindFunctionExpression, ast.KindArrowFunction:
						return true
					}
				}
			}
		}
	}
	// Fall back to type-based check: check if the type has call signatures
	typ := t.checker.GetTypeAtLocation(node)
	if typ != nil {
		callSigs := checker.Checker_getSignaturesOfType(t.checker, typ, checker.SignatureKindCall)
		if len(callSigs) > 0 {
			return true
		}
	}
	return false
}

func (t *Transpiler) isStringExpression(node *ast.Node) bool {

	typ := t.checker.GetTypeAtLocation(node)
	return t.isStringType(typ)
}

// isNonNullStringExpression returns true only if the type is definitely string
// (not string | undefined). Used in template literals where nil needs tostring().
func (t *Transpiler) isNonNullStringExpression(node *ast.Node) bool {

	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return false
	}
	flags := checker.Type_flags(typ)
	if flags&checker.TypeFlagsStringLike != 0 {
		return true
	}
	// For unions, every member must be string-like (no null/undefined)
	if flags&checker.TypeFlagsUnion != 0 {
		for _, member := range typ.Types() {
			if checker.Type_flags(member)&checker.TypeFlagsStringLike == 0 {
				return false
			}
		}
		return true
	}
	return false
}
