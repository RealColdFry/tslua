package transpiler

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// isTypeOnlyExportSpecifier checks if an export specifier targets a type-only symbol.
// For `export { foo }` where foo is a type alias or interface, returns true.
func (t *Transpiler) isTypeOnlyExportSpecifier(spec *ast.Node) bool {

	es := spec.AsExportSpecifier()
	// Resolve the local target: for `export { x as y }`, get x's symbol
	localNode := es.Name()
	if es.PropertyName != nil {
		localNode = es.PropertyName
	}
	sym := t.checker.GetSymbolAtLocation(localNode)
	if sym == nil {
		return false
	}
	// For non-alias symbols, check directly
	if sym.Flags&ast.SymbolFlagsAlias == 0 {
		return sym.Flags&ast.SymbolFlagsValue == 0
	}
	// For aliases, resolve to the target and check
	resolved := checker.Checker_getImmediateAliasedSymbol(t.checker, sym)
	if resolved == nil {
		return false
	}
	// Only treat as type-only if the resolved symbol is CLEARLY type-only
	// (TypeAlias or Interface with no Value flags)
	return resolved.Flags&ast.SymbolFlagsValue == 0 && (resolved.Flags&(ast.SymbolFlagsTypeAlias|ast.SymbolFlagsInterface)) != 0
}

// shouldBeImported checks if an import binding is actually referenced as a value.
// Type-only imports (used only in type positions) should be elided.
func (t *Transpiler) shouldBeImported(node *ast.Node) bool {

	resolver := t.checker.GetEmitResolver()
	if resolver == nil {
		return true
	}
	return resolver.IsReferencedAliasDeclaration(node)
}

// transformImportDeclaration handles all import forms.
func (t *Transpiler) transformImportDeclaration(node *ast.Node) []lua.Statement {
	importDecl := node.AsImportDeclaration()

	// Skip type-only imports entirely
	if importDecl.ImportClause != nil && importDecl.ImportClause.AsImportClause().PhaseModifier == ast.KindTypeKeyword {
		return nil
	}

	// In lualib bundle mode, skip all imports — the symbols are in scope as globals.
	// But still track imported names as lualib deps for the dependency graph.
	if t.luaLibImport == LuaLibImportNone {
		if importDecl.ImportClause != nil {
			clause := importDecl.ImportClause.AsImportClause()
			if clause.NamedBindings != nil && clause.NamedBindings.Kind == ast.KindNamedImports {
				for _, spec := range clause.NamedBindings.AsNamedImports().Elements.Nodes {
					imp := spec.AsImportSpecifier()
					if imp.IsTypeOnly {
						continue
					}
					// Skip const enum imports — they're inlined, no runtime dependency.
					sym := t.checker.GetSymbolAtLocation(imp.Name())
					if sym != nil && sym.Flags&ast.SymbolFlagsAlias != 0 {
						resolved := checker.Checker_getImmediateAliasedSymbol(t.checker, sym)
						if resolved != nil && resolved.Flags&ast.SymbolFlagsConstEnum != 0 {
							continue
						}
					}
					name := imp.Name().AsIdentifier().Text
					t.requireLualib(name)
				}
			}
		}
		return nil
	}

	specText := importDecl.ModuleSpecifier.AsStringLiteral().Text
	modulePath := t.resolveModulePath(importDecl.ModuleSpecifier)
	requireCall := lua.Call(lua.Ident("require"), lua.Str(modulePath))
	t.setNodePos(requireCall, importDecl.ModuleSpecifier)

	var result []lua.Statement

	// Side-effect only import: import "module"
	if importDecl.ImportClause == nil {
		result = []lua.Statement{lua.ExprStmt(requireCall)}
		t.registerImportStatements(result)
		return result
	}

	clause := importDecl.ImportClause.AsImportClause()
	usingRequire := false

	// Default import: import X from "module"
	if clause.Name() != nil && t.shouldBeImported(importDecl.ImportClause) {
		requireVar := safeModuleVarName(specText)
		defaultName := clause.Name().AsIdentifier().Text
		if t.hasUnsafeIdentifierName(clause.Name()) {
			defaultName = luaSafeName(defaultName)
		}
		if !usingRequire {
			result = append(result, lua.LocalDecl([]*lua.Identifier{lua.Ident(requireVar)}, []lua.Expression{requireCall}))
			usingRequire = true
		}
		result = append(result,
			lua.LocalDecl([]*lua.Identifier{lua.Ident(defaultName)}, []lua.Expression{
				lua.Index(lua.Ident(requireVar), lua.Str("default")),
			}),
		)
	}

	// Named or namespace bindings
	if clause.NamedBindings != nil {
		switch clause.NamedBindings.Kind {
		case ast.KindNamespaceImport:
			if t.shouldBeImported(clause.NamedBindings) {
				ns := clause.NamedBindings.AsNamespaceImport()
				name := ns.Name().AsIdentifier().Text
				if t.hasUnsafeIdentifierName(ns.Name()) {
					name = luaSafeName(name)
				}
				// Namespace import uses require directly as the variable
				nsDecl := lua.LocalDecl([]*lua.Identifier{lua.Ident(name)}, []lua.Expression{requireCall})
				t.setNodePos(nsDecl, ns.Name())
				result = append(result, nsDecl)
				usingRequire = true
			}

		case ast.KindNamedImports:
			requireVar := safeModuleVarName(specText)
			bindings := t.transformNamedBindings(clause.NamedBindings, requireVar)
			if len(bindings) > 0 {
				if !usingRequire {
					result = append(result, lua.LocalDecl([]*lua.Identifier{lua.Ident(requireVar)}, []lua.Expression{requireCall}))
					usingRequire = true
				}
				result = append(result, bindings...)
			}
		}
	}

	if len(result) > 0 {
		t.registerImportStatements(result)
	}
	return result
}

// transformNamedBindings returns local bindings from a require variable.
func (t *Transpiler) transformNamedBindings(bindings *ast.Node, requireVar string) []lua.Statement {
	// Namespace import: import defaultExport, * as ns from "module"
	if bindings.Kind == ast.KindNamespaceImport {
		ns := bindings.AsNamespaceImport()
		nsName := ns.Name().AsIdentifier().Text
		if t.hasUnsafeIdentifierName(ns.Name()) {
			nsName = luaSafeName(nsName)
		}
		return []lua.Statement{lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(nsName)},
			[]lua.Expression{lua.Ident(requireVar)},
		)}
	}
	if bindings.Kind != ast.KindNamedImports {
		return nil
	}
	named := bindings.AsNamedImports()
	if named.Elements == nil {
		return nil
	}
	var result []lua.Statement
	for i := range named.Elements.Nodes {
		spec := named.Elements.Nodes[i]
		if spec.AsImportSpecifier().IsTypeOnly || !t.shouldBeImported(spec) {
			continue
		}
		s := spec.AsImportSpecifier()
		localName := s.Name().AsIdentifier().Text
		safeLocalName := localName
		if t.hasUnsafeIdentifierName(s.Name()) {
			safeLocalName = luaSafeName(localName)
		}
		remoteName := localName
		if s.PropertyName != nil {
			remoteName = moduleExportName(s.PropertyName)
		}
		// @customName on the imported symbol: use the custom name for both
		// the local binding and the module property access.
		// For aliased imports (import { X as Y }), check the original name's symbol.
		lookupNode := s.Name()
		if s.PropertyName != nil {
			lookupNode = s.PropertyName
		}
		if sym := t.checker.GetSymbolAtLocation(lookupNode); sym != nil {
			if customName := t.getCustomNameFromSymbol(sym); customName != "" {
				if s.PropertyName == nil {
					// Non-aliased: rename both local and remote
					safeLocalName = customName
				}
				remoteName = customName
			}
		}
		bindingDecl := lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(safeLocalName)},
			[]lua.Expression{lua.Index(lua.Ident(requireVar), lua.Str(remoteName))},
		)
		t.setNodePos(bindingDecl, spec)
		result = append(result, bindingDecl)
	}
	return result
}

// transformImportEqualsDeclaration handles `import x = namespace.member` and `import x = require("module")`.
func (t *Transpiler) transformImportEqualsDeclaration(node *ast.Node) []lua.Statement {
	ied := node.AsImportEqualsDeclaration()

	// Skip type-only or unreferenced import equals declarations
	if ied.IsTypeOnly || !t.shouldBeImported(node) {
		return nil
	}

	name := ied.Name().AsIdentifier().Text
	if t.hasUnsafeIdentifierName(ied.Name()) {
		name = luaSafeName(name)
	}

	expr := t.transformExpression(ied.ModuleReference)
	return []lua.Statement{lua.LocalDecl(
		[]*lua.Identifier{lua.Ident(name)},
		[]lua.Expression{expr},
	)}
}

// transformExportDeclaration handles export forms.
func (t *Transpiler) transformExportDeclaration(node *ast.Node) []lua.Statement {
	exportDecl := node.AsExportDeclaration()
	// export * from "module" (no export clause)
	if exportDecl.ExportClause == nil && exportDecl.ModuleSpecifier != nil {
		modulePath := t.resolveModulePath(exportDecl.ModuleSpecifier)
		var assignTarget lua.Expression
		if t.exportAsGlobal {
			// _G[key] = value
			assignTarget = lua.Ident("_G")
		} else {
			assignTarget = lua.Ident("____exports")
		}
		innerStmts := []lua.Statement{
			lua.LocalDecl([]*lua.Identifier{lua.Ident("____export")}, []lua.Expression{
				lua.Call(lua.Ident("require"), lua.Str(modulePath)),
			}),
			lua.ForIn(
				[]*lua.Identifier{lua.Ident("____exportKey"), lua.Ident("____exportValue")},
				[]lua.Expression{lua.Call(lua.Ident("pairs"), lua.Ident("____export"))},
				&lua.Block{Statements: []lua.Statement{
					lua.If(
						lua.Binary(lua.Ident("____exportKey"), lua.OpNeq, lua.Str("default")),
						&lua.Block{Statements: []lua.Statement{
							lua.Assign(
								[]lua.Expression{lua.Index(assignTarget, lua.Ident("____exportKey"))},
								[]lua.Expression{lua.Ident("____exportValue")},
							),
						}},
						nil,
					),
				}},
			),
		}
		return []lua.Statement{lua.Do(innerStmts...)}
	}

	if exportDecl.ExportClause == nil {
		return nil
	}

	switch exportDecl.ExportClause.Kind {
	case ast.KindNamespaceExport:
		ns := exportDecl.ExportClause.AsNamespaceExport()
		name := moduleExportName(ns.Name())
		if exportDecl.ModuleSpecifier != nil {
			modulePath := t.resolveModulePath(exportDecl.ModuleSpecifier)
			// tsgo parses `export { default } from "module"` as KindNamespaceExport
			// with name "default", not as KindNamedExports. Handle this as a named
			// re-export of the module's default export.
			if name == "default" {
				requireVar := safeModuleVarName(exportDecl.ModuleSpecifier.AsStringLiteral().Text)
				return []lua.Statement{lua.Do(
					lua.LocalDecl([]*lua.Identifier{lua.Ident(requireVar)}, []lua.Expression{lua.Call(lua.Ident("require"), lua.Str(modulePath))}),
					lua.Assign(
						[]lua.Expression{lua.Index(lua.Ident("____exports"), lua.Str("default"))},
						[]lua.Expression{lua.Index(lua.Ident(requireVar), lua.Str("default"))},
					),
				)}
			}
			// export * as ns from "module"
			if t.exportAsGlobal {
				return []lua.Statement{lua.Assign(
					[]lua.Expression{lua.Ident(name)},
					[]lua.Expression{lua.Call(lua.Ident("require"), lua.Str(modulePath))},
				)}
			}
			return []lua.Statement{lua.Assign(
				[]lua.Expression{lua.Index(lua.Ident("____exports"), lua.Str(name))},
				[]lua.Expression{lua.Call(lua.Ident("require"), lua.Str(modulePath))},
			)}
		}
	case ast.KindNamedExports:
		named := exportDecl.ExportClause.AsNamedExports()
		if named.Elements == nil {
			return nil
		}

		if exportDecl.ModuleSpecifier != nil {
			// Re-export from another module
			specText := exportDecl.ModuleSpecifier.AsStringLiteral().Text
			modulePath := t.resolveModulePath(exportDecl.ModuleSpecifier)
			requireVar := safeModuleVarName(specText)
			var innerStmts []lua.Statement
			innerStmts = append(innerStmts, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(requireVar)},
				[]lua.Expression{lua.Call(lua.Ident("require"), lua.Str(modulePath))},
			))
			for _, spec := range named.Elements.Nodes {
				s := spec.AsExportSpecifier()
				exportedName := moduleExportName(s.Name())
				sourceName := exportedName
				if s.PropertyName != nil {
					sourceName = moduleExportName(s.PropertyName)
				}
				if t.exportAsGlobal {
					innerStmts = append(innerStmts, lua.Assign(
						[]lua.Expression{lua.Ident(exportedName)},
						[]lua.Expression{lua.Index(lua.Ident(requireVar), lua.Str(sourceName))},
					))
				} else {
					innerStmts = append(innerStmts, lua.Assign(
						[]lua.Expression{lua.Index(lua.Ident("____exports"), lua.Str(exportedName))},
						[]lua.Expression{lua.Index(lua.Ident(requireVar), lua.Str(sourceName))},
					))
				}
			}
			return []lua.Statement{lua.Do(innerStmts...)}
		}

		// Local re-export: export { X, Y as Z }
		var result []lua.Statement
		for _, spec := range named.Elements.Nodes {
			s := spec.AsExportSpecifier()
			// Skip type-only specifiers and type-only symbols
			if s.IsTypeOnly || exportDecl.IsTypeOnly || t.isTypeOnlyExportSpecifier(spec) {
				continue
			}
			exportedName := moduleExportName(s.Name())
			localNameNode := s.Name()
			if s.PropertyName != nil {
				localNameNode = s.PropertyName
			}
			localName := moduleExportName(localNameNode)
			if localNameNode.Kind == ast.KindIdentifier && t.hasUnsafeIdentifierName(localNameNode) {
				localName = luaSafeName(localName)
			}
			// If the local name is an exported variable, it lives on ____exports
			// (tslua rewrites exported identifiers), not as a bare local.
			var localExpr lua.Expression
			if t.isModule && !t.exportAsGlobal && t.exportedNames[localName] {
				localExpr = lua.Index(lua.Ident("____exports"), lua.Str(localName))
			} else {
				localExpr = lua.Ident(localName)
			}
			if t.exportAsGlobal {
				result = append(result, lua.Assign(
					[]lua.Expression{lua.Ident(exportedName)},
					[]lua.Expression{localExpr},
				))
			} else {
				result = append(result, lua.Assign(
					[]lua.Expression{lua.Index(lua.Ident("____exports"), lua.Str(exportedName))},
					[]lua.Expression{localExpr},
				))
			}
		}
		return result
	}
	return nil
}

// transformExportAssignment handles `export default X` and `export = X`.
func (t *Transpiler) transformExportAssignment(node *ast.Node) []lua.Statement {
	ea := node.AsExportAssignment()
	expr, prec := t.transformExprInScope(ea.Expression)
	result := make([]lua.Statement, 0, len(prec)+1)
	result = append(result, prec...)
	if ea.IsExportEquals {
		result = append(result, lua.Assign(
			[]lua.Expression{lua.Ident("____exports")},
			[]lua.Expression{expr},
		))
	} else {
		result = append(result, lua.Assign(
			[]lua.Expression{lua.Index(lua.Ident("____exports"), lua.Str("default"))},
			[]lua.Expression{expr},
		))
	}
	return result
}

// ==========================================================================
// Module name helpers
// ==========================================================================

func (t *Transpiler) resolveModulePath(moduleSpecifier *ast.Node) string {
	specText := moduleSpecifier.AsStringLiteral().Text

	// noResolvePaths: emit the specifier as-is for listed modules.
	if t.noResolvePaths[specText] {
		return specText
	}

	resolved := t.program.GetResolvedModuleFromModuleSpecifier(t.sourceFile, moduleSpecifier)
	if resolved != nil && resolved.ResolvedFileName != "" {
		moduleName := ModuleNameFromPath(resolved.ResolvedFileName, t.sourceRoot)

		// Validate that the resolved path stays within sourceRoot (rootDir).
		// A relative path with ".." segments means it escaped.
		rel, err := filepath.Rel(t.sourceRoot, resolved.ResolvedFileName)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.addError(moduleSpecifier, dw.CouldNotResolveRequire,
				fmt.Sprintf("Could not resolve lua source files for require path '%s'.", specText))
		}

		return moduleName
	}

	p := specText
	for strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
		if strings.HasPrefix(p, "./") {
			p = p[2:]
		} else {
			p = p[3:]
		}
	}
	return strings.ReplaceAll(p, "/", ".")
}

// transformImportExpression handles dynamic import() expressions.
// import("./module") → __TS__Promise.resolve(require("module"))
func (t *Transpiler) transformImportExpression(ce *ast.CallExpression) lua.Expression {
	promiseName := t.requireLualib("__TS__Promise")

	var moduleRequire lua.Expression
	args := ce.Arguments.Nodes
	if len(args) > 0 {
		modulePath := t.resolveModulePath(args[0])
		moduleRequire = lua.Call(lua.Ident("require"), lua.Str(modulePath))
	} else {
		moduleRequire = lua.Nil()
	}

	// __TS__Promise["resolve"](require("module"))
	return lua.Call(lua.Index(lua.Ident(promiseName), lua.Str("resolve")), moduleRequire)
}

func normalizeModulePath(p string) string {
	if strings.HasPrefix(p, "./") {
		return p[2:]
	}
	return p
}

func safeModuleVarName(modulePath string) string {
	base := path.Base(modulePath)
	base = strings.Trim(base, `"'`)

	var sb strings.Builder
	sb.WriteString("____")
	for _, ch := range base {
		if isLuaIdentChar(ch) {
			sb.WriteRune(ch)
		} else {
			fmt.Fprintf(&sb, "_%02X", ch)
		}
	}
	return sb.String()
}

func isLuaIdentStartChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isLuaIdentChar(ch rune) bool {
	return isLuaIdentStartChar(ch) || (ch >= '0' && ch <= '9')
}

func moduleExportName(node *ast.Node) string {
	switch node.Kind {
	case ast.KindIdentifier:
		return node.AsIdentifier().Text
	case ast.KindStringLiteral:
		return node.AsStringLiteral().Text
	case ast.KindDefaultKeyword:
		return "default"
	default:
		return node.AsIdentifier().Text
	}
}

// createModuleLocalNameIdentifier returns the safe Lua identifier name for a module declaration.
func (t *Transpiler) createModuleLocalNameIdentifier(md *ast.Node) string {
	name := md.AsModuleDeclaration().Name().AsIdentifier().Text
	if customName := t.getCustomName(md); customName != "" {
		name = customName
	}
	if t.isLuaUnsafeName(name) {
		return luaSafeName(name)
	}
	return name
}

// createModuleLocalName builds the full Lua expression for a module declaration's local reference.
// For top-level namespaces, returns an identifier (e.g., Foo).
// For nested dotted namespaces (namespace Foo.Bar), returns a table index chain (e.g., Foo["Bar"]).
func (t *Transpiler) createModuleLocalName(module *ast.Node) lua.Expression {
	if module.Parent != nil && module.Parent.Kind != ast.KindSourceFile && module.Parent.Kind == ast.KindModuleDeclaration {
		parentExpr := t.createModuleLocalName(module.Parent)
		name := t.createModuleLocalNameIdentifier(module)
		return lua.Index(parentExpr, lua.Str(name))
	}
	return lua.Ident(t.createModuleLocalNameIdentifier(module))
}

func (t *Transpiler) transformModuleDeclaration(node *ast.Node) []lua.Statement {
	md := node.AsModuleDeclaration()
	if md.Name() == nil {
		return nil
	}
	name := t.createModuleLocalNameIdentifier(node)
	origNsName := md.Name().AsIdentifier().Text
	isExported := hasExportModifier(node)

	// nsIdent creates a namespace name identifier with source map position and name.
	nsIdent := func() *lua.Identifier {
		id := lua.Ident(name)
		if name != origNsName {
			t.setNodePosNamed(id, md.Name(), origNsName)
		}
		return id
	}

	// Check if this is the first declaration of this namespace (supports merging)
	isFirst := t.isFirstDeclaration(node, md.Name())

	var result []lua.Statement

	// Non-module namespace could be merged if:
	// - is top level
	// - is nested and exported
	var exportTarget lua.Expression
	if isExported && t.currentNamespaceNode != nil {
		exportTarget = t.createModuleLocalName(t.currentNamespaceNode)
	} else if isExported && (t.isModule || t.currentNamespace != "") {
		exportTarget = lua.Ident("____exports")
	}
	hasExportTarget := exportTarget != nil
	isNonModuleMergeable := !t.isModule && (t.currentNamespace == "" || hasExportTarget)

	// Declare the namespace table
	if isNonModuleMergeable {
		// Use mergeable pattern: X = X or {}
		if hasExportTarget {
			origName := md.Name().AsIdentifier().Text
			exportedRef := lua.Index(exportTarget, lua.Str(origName))
			result = append(result, lua.Assign(
				[]lua.Expression{exportedRef},
				[]lua.Expression{lua.Binary(exportedRef, lua.OpOr, lua.Table())},
			))
			// Local alias — register for hoisting
			decl := lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(name)},
				[]lua.Expression{lua.Index(exportTarget, lua.Str(origName))},
			)
			if t.inScope() {
				if sym := t.checker.GetSymbolAtLocation(md.Name()); sym != nil {
					symID := t.getOrCreateSymbolID(sym)
					t.addScopeVariableDeclaration(decl, symID)
				}
			}
			result = append(result, decl)
		} else if t.shouldUseLocalDeclaration() {
			decl := lua.LocalDecl(
				[]*lua.Identifier{nsIdent()},
				[]lua.Expression{lua.Binary(lua.Ident(name), lua.OpOr, lua.Table())},
			)
			// Register for hoisting consideration
			if t.inScope() {
				if sym := t.checker.GetSymbolAtLocation(md.Name()); sym != nil {
					symID := t.getOrCreateSymbolID(sym)
					t.addScopeVariableDeclaration(decl, symID)
				}
			}
			result = append(result, decl)
		} else {
			// Global namespace: use mergeable pattern A = A or ({})
			result = append(result, lua.Assign(
				[]lua.Expression{nsIdent()},
				[]lua.Expression{lua.Binary(lua.Ident(name), lua.OpOr, lua.Table())},
			))
		}
	} else if isFirst {
		// Module context: only emit on first declaration
		if hasExportTarget {
			origNameStr := md.Name().AsIdentifier().Text
			result = append(result, lua.Assign(
				[]lua.Expression{lua.Index(exportTarget, lua.Str(origNameStr))},
				[]lua.Expression{lua.Table()},
			))
			// Local alias — register for hoisting
			decl := lua.LocalDecl(
				[]*lua.Identifier{nsIdent()},
				[]lua.Expression{lua.Index(exportTarget, lua.Str(origNameStr))},
			)
			if t.inScope() {
				if sym := t.checker.GetSymbolAtLocation(md.Name()); sym != nil {
					symID := t.getOrCreateSymbolID(sym)
					t.addScopeVariableDeclaration(decl, symID)
				}
			}
			result = append(result, decl)
		} else {
			decl := lua.LocalDecl(
				[]*lua.Identifier{nsIdent()},
				[]lua.Expression{lua.Table()},
			)
			if t.inScope() {
				if sym := t.checker.GetSymbolAtLocation(md.Name()); sym != nil {
					symID := t.getOrCreateSymbolID(sym)
					t.addScopeVariableDeclaration(decl, symID)
				}
			}
			result = append(result, decl)
		}
	}

	// Transform the module body — set namespace context before processing either body type
	if md.Body != nil {
		prevNamespace := t.currentNamespace
		prevNamespaceNode := t.currentNamespaceNode
		t.currentNamespace = name
		t.currentNamespaceNode = node
		var bodyStmts []lua.Statement
		switch md.Body.Kind {
		case ast.KindModuleBlock:
			mb := md.Body.AsModuleBlock()
			scope := t.pushScope(ScopeBlock, md.Body)
			for _, stmt := range mb.Statements.Nodes {
				bodyStmts = append(bodyStmts, t.transformStatement(stmt)...)
			}
			bodyStmts = t.performHoisting(scope, bodyStmts)
			t.popScope()
		case ast.KindModuleDeclaration:
			bodyStmts = t.transformModuleDeclaration(md.Body)
		}
		t.currentNamespace = prevNamespace
		t.currentNamespaceNode = prevNamespaceNode
		if len(bodyStmts) > 0 {
			result = append(result, lua.Do(bodyStmts...))
		}
	}

	return result
}
