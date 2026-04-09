// Unified self-detection system. Both functionNeedsSelf (definition side) and
// calleeNeedsSelf (call site) route through computeDeclarationContextType.
package transpiler

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/microsoft/typescript-go/shim/compiler"
)

type contextType int

const (
	contextNone    contextType = 0
	contextVoid    contextType = 1 << 0
	contextNonVoid contextType = 1 << 1
	contextMixed   contextType = contextVoid | contextNonVoid
)

// computeDeclarationContextType determines the self-context from a signature declaration.
// This is the single source of truth for whether a function uses self.
func (t *Transpiler) computeDeclarationContextType(signatureDecl *ast.Node) contextType {
	// 1. Explicit 'this' parameter
	thisKind := getThisParamKindFromSignature(signatureDecl)
	if thisKind != thisParamNone {
		if thisKind == thisParamVoid {
			return contextVoid
		}
		return contextNonVoid
	}

	// 2. @noSelf on the signature declaration itself
	if t.nodeHasNoSelf(signatureDecl) {
		return contextVoid
	}

	// 3. Method/constructor/property in class/interface → check parent @noSelf, else NonVoid
	if isMethodLikeDeclaration(signatureDecl) {
		// Find enclosing class or interface
		parent := signatureDecl.Parent
		for parent != nil {
			switch parent.Kind {
			case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration:
				if t.nodeHasNoSelf(parent) {
					return contextVoid
				}
				return contextNonVoid
			}
			parent = parent.Parent
		}
		return contextNonVoid
	}

	// 4. Type parameter parent → NonVoid
	if signatureDecl.Parent != nil && signatureDecl.Parent.Kind == ast.KindTypeParameter {
		return contextNonVoid
	}

	// 5. noImplicitSelf option (TSTL lines 148-160)
	// When enabled, user source files default to no-self. Default libs and external
	// libraries (node_modules) are unaffected so built-in methods keep self.
	if t.noImplicitSelf && t.program != nil {
		sf := ast.GetSourceFileOfNode(signatureDecl)
		if sf != nil &&
			!compiler.Program_IsSourceFileDefaultLibrary(t.program, sf.Path()) &&
			!compiler.Program_IsSourceFileFromExternalLibrary(t.program, sf) {
			return contextVoid
		}
	}

	// 6. Walk up to find @noSelf or @noSelfInFile
	if t.hasNoSelfAncestor(signatureDecl) {
		return contextVoid
	}

	// 7. Default
	return contextNonVoid
}

// isMethodLikeDeclaration returns true for nodes that are "method-like" in the
// TSTL sense: methods, constructors, or function types nested inside property/index
// signature declarations in a class or interface.
func isMethodLikeDeclaration(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindMethodSignature, ast.KindMethodDeclaration,
		ast.KindConstructSignature, ast.KindConstructor:
		return true
	}
	if node.Parent != nil {
		switch node.Parent.Kind {
		case ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindIndexSignature:
			return true
		}
	}
	return false
}

// nodeHasNoSelf checks @noSelf on a single node, including its declaration host.
// For class expressions used as variable initializers (e.g., `/** @noSelf */ const X = class {}`),
// the JSDoc is attached to the VariableStatement, not the ClassExpression. This mirrors
// TypeScript's getAllJSDocTags which walks through the declaration host.
func (t *Transpiler) nodeHasNoSelf(node *ast.Node) bool {
	sf := ast.GetSourceFileOfNode(node)
	if sf == nil {
		sf = t.sourceFile
	}
	if hasNodeAnnotation(node, sf, AnnotNoSelf) || hasAnnotationInLeadingTrivia(node, sf, "noself") {
		return true
	}
	// Check the declaration host: ClassExpression → VariableDeclaration → VariableDeclarationList → VariableStatement
	if node.Kind == ast.KindClassExpression && node.Parent != nil &&
		node.Parent.Kind == ast.KindVariableDeclaration {
		varDeclList := node.Parent.Parent
		if varDeclList != nil && varDeclList.Kind == ast.KindVariableDeclarationList {
			varStmt := varDeclList.Parent
			if varStmt != nil && varStmt.Kind == ast.KindVariableStatement {
				if hasNodeAnnotation(varStmt, sf, AnnotNoSelf) || hasAnnotationInLeadingTrivia(varStmt, sf, "noself") {
					return true
				}
			}
		}
	}
	return false
}

// hasNoSelfAncestor walks up from declaration to find @noSelfInFile on the source file
// or @noSelf on enclosing module declarations.
func (t *Transpiler) hasNoSelfAncestor(declaration *ast.Node) bool {
	node := declaration
	for {
		// Find first SourceFile or ModuleDeclaration above node
		parent := node.Parent
		for parent != nil {
			if parent.Kind == ast.KindSourceFile || parent.Kind == ast.KindModuleDeclaration {
				break
			}
			parent = parent.Parent
		}
		if parent == nil {
			return false
		}
		if parent.Kind == ast.KindSourceFile {
			sf := ast.GetSourceFileOfNode(parent)
			if sf == nil {
				sf = t.sourceFile
			}
			return hasFileAnnotation(sf, AnnotNoSelfInFile)
		}
		// ModuleDeclaration — check @noSelf
		if t.nodeHasNoSelf(parent) {
			return true
		}
		// Recurse upward from the module declaration
		node = parent
	}
}

// getDeclarationContextType determines context type for a function definition node.
// Handles contextual type resolution for arrows, function expressions, and methods
// in object literals before falling back to computeDeclarationContextType.
func (t *Transpiler) getDeclarationContextType(node *ast.Node) contextType {

	// If explicit this parameter exists, skip contextual type resolution
	thisKind := getThisParamKindFromSignature(node)
	if thisKind != thisParamNone {
		if thisKind == thisParamVoid {
			return contextVoid
		}
		return contextNonVoid
	}

	// Contextual type resolution for arrows, function expressions, and object literal methods
	var inferredType *checker.Type
	if node.Kind == ast.KindMethodDeclaration && node.Parent != nil && node.Parent.Kind == ast.KindObjectLiteralExpression {
		inferredType = t.checker.GetContextualTypeForObjectLiteralElement(node, 0)
	} else if node.Kind == ast.KindArrowFunction || node.Kind == ast.KindFunctionExpression {
		// Only use getContextualType — do NOT fall back to getTypeAtLocation.
		// TSTL's inferAssignedType does fallback, but it causes union types to
		// resolve to this: void member signatures incorrectly.
		inferredType = checker.Checker_getContextualType(t.checker, node, 0)
	}

	if inferredType != nil {
		sigs := getAllCallSignatures(t.checker, inferredType)
		if len(sigs) > 0 {
			ct := contextNone
			for _, sig := range sigs {
				decl := checker.Signature_declaration(sig)
				if decl != nil {
					ct |= t.computeDeclarationContextType(decl)
				}
			}
			if ct != contextNone {
				return ct
			}
		}
	}

	return t.computeDeclarationContextType(node)
}

// getCallContextType determines context type for a call expression.
func (t *Transpiler) getCallContextType(node *ast.Node) contextType {

	sig := t.checker.GetResolvedSignature(node)
	if sig != nil {
		decl := checker.Signature_declaration(sig)
		if decl != nil {
			return t.computeDeclarationContextType(decl)
		}
	}

	// No signature declaration resolved — check root declarations for @noSelfInFile
	calledExpr := getCalledExpression(node)
	if calledExpr != nil {
		declarations := t.findRootDeclarations(calledExpr)
		for _, decl := range declarations {
			sf := ast.GetSourceFileOfNode(decl)
			if sf != nil && hasFileAnnotation(sf, AnnotNoSelfInFile) {
				return contextVoid
			}
		}
	}

	// Fallback: check if the callee's type has call signatures whose declarations
	// are in @noSelfInFile files.
	if calledExpr != nil {
		typ := t.checker.GetTypeAtLocation(calledExpr)
		if typ != nil {
			typ = checker.Checker_GetNonNullableType(t.checker, typ)
			sigs := getAllCallSignatures(t.checker, typ)
			ct := contextNone
			for _, s := range sigs {
				d := checker.Signature_declaration(s)
				if d != nil {
					ct |= t.computeDeclarationContextType(d)
				}
			}
			if ct != contextNone {
				return ct
			}
		}
	}

	// noImplicitSelf: when enabled, unresolved calls default to void context (no self).
	if t.noImplicitSelf {
		return contextVoid
	}
	return contextNonVoid
}

// getCalledExpression extracts the expression being called from a call-like expression.
func getCalledExpression(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindCallExpression:
		return node.AsCallExpression().Expression
	case ast.KindTaggedTemplateExpression:
		return node.AsTaggedTemplateExpression().Tag
	case ast.KindJsxSelfClosingElement:
		return node.TagName()
	case ast.KindJsxOpeningElement:
		return node.TagName()
	}
	return nil
}

// findRootDeclarations resolves through import aliases to find the original declarations.
func (t *Transpiler) findRootDeclarations(calledExpr *ast.Node) []*ast.Node {
	calledSym := t.checker.GetSymbolAtLocation(calledExpr)
	if calledSym == nil {
		return nil
	}

	var result []*ast.Node
	for _, d := range calledSym.Declarations {
		if d.Kind == ast.KindImportSpecifier {
			if aliased := checker.Checker_getImmediateAliasedSymbol(t.checker, calledSym); aliased != nil {
				result = append(result, aliased.Declarations...)
			}
		} else {
			result = append(result, d)
		}
	}
	return result
}

// getAllCallSignatures returns call signatures, recursing into union types.
func getAllCallSignatures(ch *checker.Checker, typ *checker.Type) []*checker.Signature {
	flags := checker.Type_flags(typ)
	if flags&checker.TypeFlagsUnion != 0 {
		var sigs []*checker.Signature
		for _, member := range typ.Types() {
			sigs = append(sigs, getAllCallSignatures(ch, member)...)
		}
		return sigs
	}
	return checker.Checker_getSignaturesOfType(ch, typ, checker.SignatureKindCall)
}
