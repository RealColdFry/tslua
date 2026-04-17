package transpiler

import (
	"fmt"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/microsoft/typescript-go/shim/compiler"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// RuntimeAdapters holds per-category runtime adapter configuration for a
// program. Every adapter slot is always populated: the default emits native
// Lua operators (e.g. `#arr`) via Go AST construction; a user-provided
// @lua*Runtime declaration replaces the default with a call to a named Lua
// function.
//
// Kernel scope: only Array.length is wired. See notes/adapters/architecture.md.
type RuntimeAdapters struct {
	Array ArrayAdapter

	// HasUserAdapter reports whether any user declaration overrode a default.
	// The CLI uses this to decide whether to rebuild lualib from source.
	HasUserAdapter bool
}

// Any reports whether any user declaration overrode a default emitter.
// Callers use it to decide whether to rebuild lualib from source (cost:
// one full lualib pass) or use the embedded pre-built bundle (zero cost).
// Safe to call on a nil receiver: nil adapters means "no user adapter".
func (r *RuntimeAdapters) Any() bool {
	return r != nil && r.HasUserAdapter
}

// NewDefaultAdapters returns a RuntimeAdapters populated with tslua's built-in
// emitters. Every subsequent lookup on the returned value emits the native
// Lua operator or lualib call that tslua would emit without any adapter.
func NewDefaultAdapters() *RuntimeAdapters {
	return &RuntimeAdapters{
		Array: ArrayAdapter{
			Length: defaultLengthEmitter{},
		},
	}
}

// ArrayAdapter names the emitters for Array primitives.
type ArrayAdapter struct {
	// Length emits the Lua expression for `arr.length` on array-typed
	// expressions. Default: `#arr` (or `table.getn(arr)` for Lua 5.0).
	Length LengthEmitter
}

// LengthEmitter produces a Lua expression for an array length read.
// Implementations are branchless on the caller side: every emit site calls
// `t.adapters.Array.Length.Emit(t, arr)` and lets the emitter decide whether
// to emit a native operator or a function call.
type LengthEmitter interface {
	Emit(t *Transpiler, arr lua.Expression) lua.Expression
}

// defaultLengthEmitter emits tslua's default length operation: `#arr` on
// most targets, `table.getn(arr)` on Lua 5.0.
type defaultLengthEmitter struct{}

func (defaultLengthEmitter) Emit(t *Transpiler, arr lua.Expression) lua.Expression {
	return t.luaTarget.LenExpr(arr)
}

// userLengthEmitter emits a call to a user-provided Lua function,
// e.g. `Len(arr)` for embedded hosts whose proxy arrays track length
// separately from Lua's raw table length.
type userLengthEmitter struct {
	FnName string
}

func (u userLengthEmitter) Emit(_ *Transpiler, arr lua.Expression) lua.Expression {
	return lua.Call(lua.Ident(u.FnName), arr)
}

// primitiveSignature describes the expected shape of an adapter function.
// The scanner uses this to validate that a user-declared function referenced
// by a @lua*Runtime const actually has the right signature before installing
// it as an adapter. On mismatch, the default emitter is retained and a
// diagnostic is emitted, preventing silent garbage at runtime.
type primitiveSignature struct {
	// Category is a human-readable label used in diagnostics ("Array.length").
	Category string
	// ParamCheck checks the type of the first argument. Returns "" on match,
	// a reason string on mismatch (used verbatim in the diagnostic).
	ParamCheck func(ch *checker.Checker, t *checker.Type) string
	// ReturnCheck checks the return type.
	ReturnCheck func(ch *checker.Checker, t *checker.Type) string
}

var arrayLengthPrimitive = primitiveSignature{
	Category:    "Array.length",
	ParamCheck:  expectArrayType,
	ReturnCheck: expectNumberLike,
}

// ScanAdaptersFromProgram is a convenience wrapper around ScanAdapters that
// obtains a checker from the program itself. Intended for callers (e.g. the
// CLI) that want the scanned adapters without managing checker lifecycle.
func ScanAdaptersFromProgram(program *compiler.Program) (*RuntimeAdapters, []*ast.Diagnostic) {
	var ch *checker.Checker
	compiler.Program_ForEachCheckerParallel(program, func(_ int, c *checker.Checker) {
		ch = c
	})
	return ScanAdapters(program, ch)
}

// ScanAdapters inspects a program for @lua*Runtime adapter declarations and
// returns a populated RuntimeAdapters plus any diagnostics from signature
// validation. The result always contains working emitters: slots the user did
// not override, or whose user declaration failed validation, keep tslua's
// defaults.
//
// Expected declaration form:
//
//	declare function Len(arr: readonly unknown[]): number;
//
//	/** @luaArrayRuntime */
//	declare const MyArrayRuntime: {
//	    length: typeof Len;
//	};
//
// A ch of nil skips validation (used by callers that cannot supply a checker).
func ScanAdapters(program *compiler.Program, ch *checker.Checker) (*RuntimeAdapters, []*ast.Diagnostic) {
	adapters := NewDefaultAdapters()
	var diags []*ast.Diagnostic
	for _, sf := range program.SourceFiles() {
		if sf.Statements == nil {
			continue
		}
		for _, stmt := range sf.Statements.Nodes {
			if stmt.Kind != ast.KindVariableStatement {
				continue
			}
			if !hasJSDocTag(stmt, sf, "luaarrayruntime") {
				continue
			}
			diags = append(diags, applyArrayAdapter(stmt, sf, ch, adapters)...)
		}
	}
	return adapters, diags
}

// applyArrayAdapter reads the type literal of a @luaArrayRuntime-tagged
// `declare const` and installs the declared primitive emitters into
// adapters.Array. A declaration whose signature does not match the primitive's
// expected shape produces a diagnostic; the default emitter is retained for
// that slot so the transpile output is always valid Lua (just not adapted).
func applyArrayAdapter(varStmt *ast.Node, sf *ast.SourceFile, ch *checker.Checker, adapters *RuntimeAdapters) []*ast.Diagnostic {
	vs := varStmt.AsVariableStatement()
	if vs.DeclarationList == nil {
		return nil
	}
	declList := vs.DeclarationList.AsVariableDeclarationList()
	if declList.Declarations == nil || len(declList.Declarations.Nodes) == 0 {
		return nil
	}
	decl := declList.Declarations.Nodes[0].AsVariableDeclaration()
	if decl.Type == nil || !ast.IsTypeLiteralNode(decl.Type) {
		return nil
	}
	lit := decl.Type.AsTypeLiteralNode()
	if lit.Members == nil {
		return nil
	}

	var diags []*ast.Diagnostic
	for _, member := range lit.Members.Nodes {
		if !ast.IsPropertySignatureDeclaration(member) {
			continue
		}
		ps := member.AsPropertySignatureDeclaration()
		nameNode := ps.Name()
		if nameNode == nil || nameNode.Kind != ast.KindIdentifier {
			continue
		}
		if ps.Type == nil {
			continue
		}
		fnName := typeQueryIdentifier(ps.Type)
		if fnName == "" {
			continue
		}
		switch nameNode.AsIdentifier().Text {
		case "length":
			if reason := validatePrimitive(ch, ps.Type, arrayLengthPrimitive); reason != "" {
				diags = append(diags, dw.NewErrorForNode(sf, member, dw.RuntimeAdapterInvalidSignature,
					fmt.Sprintf("@luaArrayRuntime primitive 'length' has invalid signature: %s", reason)))
				continue
			}
			adapters.Array.Length = userLengthEmitter{FnName: fnName}
			adapters.HasUserAdapter = true
		}
	}
	return diags
}

// validatePrimitive checks that the function referenced by a `typeof X` type
// query has a signature compatible with the expected primitive shape.
// Returns "" on match, or a reason string on mismatch.
//
// A nil checker skips validation (returns ""); callers without a checker
// trust the declaration.
func validatePrimitive(ch *checker.Checker, typeQueryNode *ast.Node, spec primitiveSignature) string {
	if ch == nil {
		return ""
	}
	tq := typeQueryNode.AsTypeQueryNode()
	if tq.ExprName == nil {
		return "missing function reference"
	}

	typ := ch.GetTypeAtLocation(tq.ExprName)
	if typ == nil {
		return "unable to resolve type of " + tq.ExprName.AsIdentifier().Text
	}

	sigs := checker.Checker_getSignaturesOfType(ch, typ, checker.SignatureKindCall)
	if len(sigs) == 0 {
		return "not a callable function"
	}
	sig := sigs[0]
	params := checker.Signature_parameters(sig)
	if len(params) < 1 {
		return "expected at least 1 parameter"
	}
	paramType := checker.Checker_getTypeOfSymbol(ch, params[0])
	if reason := spec.ParamCheck(ch, paramType); reason != "" {
		return "parameter 1: " + reason
	}
	retType := checker.Checker_getReturnTypeOfSignature(ch, sig)
	if reason := spec.ReturnCheck(ch, retType); reason != "" {
		return "return type: " + reason
	}
	return ""
}

// expectArrayType reports "" when typ is an array, tuple, or has an array
// base, matching the shapes the transpiler already recognizes as array-like.
func expectArrayType(ch *checker.Checker, typ *checker.Type) string {
	if typ == nil {
		return "unresolved type"
	}
	stripped := checker.Checker_GetNonNullableType(ch, typ)
	if isArrayLikeForAdapter(ch, stripped) {
		return ""
	}
	return "expected an array type (T[] or readonly T[])"
}

// expectNumberLike reports "" when typ is number, a numeric literal, or has
// a numeric base constraint.
func expectNumberLike(ch *checker.Checker, typ *checker.Type) string {
	if typ == nil {
		return "unresolved type"
	}
	stripped := checker.Checker_GetNonNullableType(ch, typ)
	if checker.Type_flags(stripped)&checker.TypeFlagsNumberLike != 0 {
		return ""
	}
	base := checker.Checker_getBaseConstraintOfType(ch, stripped)
	if base != nil && checker.Type_flags(base)&checker.TypeFlagsNumberLike != 0 {
		return ""
	}
	return "expected number"
}

// isArrayLikeForAdapter is a scope-limited array check for scanner validation.
// It intentionally does not replicate the full isArrayTypeFromType lattice
// (generics, intersections, indexed access, etc.): adapter signatures should
// be stated in plain T[] / readonly T[] form. Exotic cases fall through to
// a diagnostic rather than silently passing.
func isArrayLikeForAdapter(ch *checker.Checker, typ *checker.Type) bool {
	if checker.Type_flags(typ)&checker.TypeFlagsObject == 0 {
		return false
	}
	return checker.Checker_isArrayOrTupleType(ch, typ)
}

// hasJSDocTag reports whether a node carries a JSDoc @tagName (case-insensitive).
// Standalone variant of Transpiler.hasAnnotationTag, usable at program scan
// time before any Transpiler instance exists.
func hasJSDocTag(node *ast.Node, sf *ast.SourceFile, tagName string) bool {
	if node.Flags&ast.NodeFlagsHasJSDoc == 0 {
		return false
	}
	for _, jsDoc := range node.JSDoc(sf) {
		if jsDoc.Kind != ast.KindJSDoc {
			continue
		}
		tags := jsDoc.AsJSDoc().Tags
		if tags == nil {
			continue
		}
		for _, tag := range tags.Nodes {
			if !ast.IsJSDocUnknownTag(tag) {
				continue
			}
			name := tag.AsJSDocUnknownTag().TagName
			if name == nil {
				continue
			}
			if strings.ToLower(name.AsIdentifier().Text) == tagName {
				return true
			}
		}
	}
	return false
}

// typeQueryIdentifier returns the identifier from a `typeof X` type query.
// Returns "" for any other shape (qualified names like `typeof ns.X`, import
// types, unresolved references).
func typeQueryIdentifier(typeNode *ast.Node) string {
	if typeNode.Kind != ast.KindTypeQuery {
		return ""
	}
	tq := typeNode.AsTypeQueryNode()
	if tq.ExprName == nil || tq.ExprName.Kind != ast.KindIdentifier {
		return ""
	}
	return tq.ExprName.AsIdentifier().Text
}
