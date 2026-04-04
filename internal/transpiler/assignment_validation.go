// Validates function context (self/no-self) compatibility on assignments.
package transpiler

import (
	"fmt"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/microsoft/typescript-go/shim/checkershim"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
)

// typePair uniquely identifies a (from, to) type pair for recursion guarding.
type typePair struct {
	from, to *checker.Type
}

// getFunctionContextType determines the context type (self vs void) for a ts.Type.
func (t *Transpiler) getFunctionContextType(typ *checker.Type) contextType {
	flags := checker.Type_flags(typ)

	// Type parameters: check constraint
	if flags&checker.TypeFlagsTypeParameter != 0 {
		constraint := checker.Checker_getBaseConstraintOfType(t.checker, typ)
		if constraint != nil {
			return t.getFunctionContextType(constraint)
		}
	}

	sigs := checker.Checker_getSignaturesOfType(t.checker, typ, checker.SignatureKindCall)
	if len(sigs) == 0 {
		return contextNone
	}

	ct := contextNone
	for _, sig := range sigs {
		ct |= t.getSignatureContext(sig)
		if ct == contextMixed {
			break
		}
	}
	return ct
}

// getSignatureContext returns the context for a signature, flattening composite
// signatures (from union/intersection types) into their individual declarations.
func (t *Transpiler) getSignatureContext(sig *checker.Signature) contextType {
	// Flatten composite signatures (union/intersection types)
	if comp := checkershim.Signature_composite(sig); comp != nil {
		ct := contextNone
		for _, cs := range checkershim.CompositeSignature_signatures(comp) {
			ct |= t.getSignatureContext(cs)
			if ct == contextMixed {
				break
			}
		}
		return ct
	}
	decl := checker.Signature_declaration(sig)
	if decl == nil {
		return contextNone
	}
	return t.resolveSignatureContext(decl)
}

// resolveSignatureContext determines the context for a call signature declaration.
// For function expressions and arrow functions without explicit `this`, resolves
// through the contextual type to find the declared type's context (e.g., this: void
// from a type annotation). Without this, tsgo's signature declarations pointing to
// function expression initializers would incorrectly return NonVoid.
func (t *Transpiler) resolveSignatureContext(decl *ast.Node) contextType {
	thisKind := getThisParamKindFromSignature(decl)
	if thisKind == thisParamNone {
		var inferredType *checker.Type
		if decl.Kind == ast.KindMethodDeclaration && decl.Parent != nil &&
			decl.Parent.Kind == ast.KindObjectLiteralExpression {
			inferredType = t.checker.GetContextualTypeForObjectLiteralElement(decl, 0)
		} else if decl.Kind == ast.KindFunctionExpression || decl.Kind == ast.KindArrowFunction {
			inferredType = checker.Checker_getContextualType(t.checker, decl, 0)
		}
		if inferredType != nil {
			inferredSigs := getAllCallSignatures(t.checker, inferredType)
			if len(inferredSigs) > 0 {
				ct := contextNone
				for _, is := range inferredSigs {
					d := checker.Signature_declaration(is)
					if d != nil {
						ct |= t.computeDeclarationContextType(d)
					}
				}
				if ct != contextNone {
					return ct
				}
			}
		}
	}
	return t.computeDeclarationContextType(decl)
}

// validateAssignment checks function context compatibility between two types.
func (t *Transpiler) validateAssignment(node *ast.Node, fromType, toType *checker.Type, toName string) {
	if toType == fromType {
		return
	}
	if checker.Type_flags(toType)&checker.TypeFlagsAny != 0 {
		return
	}

	// Guard against infinite recursion on recursive types.
	// Uses a per-call-stack set (defer delete) so the same type pair can be
	// checked for different assignment sites within one file.
	pair := typePair{fromType, toType}
	if t.validationStack != nil && t.validationStack[pair] {
		return
	}
	if t.validationStack == nil {
		t.validationStack = make(map[typePair]bool)
	}
	t.validationStack[pair] = true
	defer delete(t.validationStack, pair)

	t.validateFunctionAssignment(node, fromType, toType, toName)

	// Recurse into arrays/tuples
	if (checker.Checker_isArrayType(t.checker, toType) || checker.IsTupleType(toType)) &&
		(checker.Checker_isArrayType(t.checker, fromType) || checker.IsTupleType(fromType)) {
		fromArgs := checker.Checker_getTypeArguments(t.checker, fromType)
		toArgs := checker.Checker_getTypeArguments(t.checker, toType)
		count := len(fromArgs)
		if len(toArgs) < count {
			count = len(toArgs)
		}
		for i := 0; i < count; i++ {
			t.validateAssignment(node, fromArgs[i], toArgs[i], toName)
		}
	}

	// Recurse into interface/object members
	fromSym := checker.Type_symbol(fromType)
	toSym := checker.Type_symbol(toType)
	if fromSym != nil && toSym != nil {
		fromMembers := fromSym.Members
		toMembers := toSym.Members
		if fromMembers != nil && toMembers != nil {
			if len(toMembers) < len(fromMembers) {
				for name, toMember := range toMembers {
					if fromMember, ok := fromMembers[name]; ok {
						toMemberType := checker.Checker_getTypeOfSymbol(t.checker, toMember)
						fromMemberType := checker.Checker_getTypeOfSymbol(t.checker, fromMember)
						memberName := name
						if toName != "" {
							memberName = toName + "." + name
						}
						t.validateAssignment(node, fromMemberType, toMemberType, memberName)
					}
				}
			} else {
				for name, fromMember := range fromMembers {
					if toMember, ok := toMembers[name]; ok {
						toMemberType := checker.Checker_getTypeOfSymbol(t.checker, toMember)
						fromMemberType := checker.Checker_getTypeOfSymbol(t.checker, fromMember)
						memberName := name
						if toName != "" {
							memberName = toName + "." + name
						}
						t.validateAssignment(node, fromMemberType, toMemberType, memberName)
					}
				}
			}
		}
	}
}

// validateFunctionAssignment checks that function context types are compatible.
func (t *Transpiler) validateFunctionAssignment(node *ast.Node, fromType, toType *checker.Type, toName string) {
	fromContext := t.getFunctionContextType(fromType)
	toContext := t.getFunctionContextType(toType)

	if fromContext == contextMixed || toContext == contextMixed {
		t.addUnsupportedOverloadAssignment(node, toName)
	} else if fromContext != toContext && fromContext != contextNone && toContext != contextNone {
		if toContext == contextVoid {
			t.addUnsupportedNoSelfFunctionConversion(node, toName)
		} else {
			t.addUnsupportedSelfFunctionConversion(node, toName)
		}
	}
}

// validateBinaryAssignment validates function context for `x = y` assignment statements.
// Uses getTypeOfSymbol for the LHS to get the declared type rather than the
// flow-narrowed type that getTypeAtLocation may return.
func (t *Transpiler) validateBinaryAssignment(be *ast.BinaryExpression) {
	if t.checker == nil {
		return
	}
	rightType := t.checker.GetTypeAtLocation(be.Right)
	var leftType *checker.Type
	lhsSym := t.checker.GetSymbolAtLocation(be.Left)
	if lhsSym != nil {
		leftType = checker.Checker_getTypeOfSymbol(t.checker, lhsSym)
	} else {
		leftType = t.checker.GetTypeAtLocation(be.Left)
	}
	if rightType != nil && leftType != nil {
		t.validateAssignment(be.Right, rightType, leftType, "")
	}
}

// validateTypeAssertion checks function context compatibility on as/type assertion expressions.
func (t *Transpiler) validateTypeAssertion(node *ast.Node) {
	if t.checker == nil {
		return
	}
	var exprNode, typeNode *ast.Node
	switch node.Kind {
	case ast.KindAsExpression:
		ae := node.AsAsExpression()
		exprNode = ae.Expression
		typeNode = ae.Type
	case ast.KindTypeAssertionExpression:
		ta := node.AsTypeAssertion()
		exprNode = ta.Expression
		typeNode = ta.Type
	default:
		return
	}
	if ast.IsConstTypeReference(typeNode) {
		return
	}
	fromType := t.checker.GetTypeAtLocation(exprNode)
	toType := t.checker.GetTypeAtLocation(typeNode)
	if fromType != nil && toType != nil {
		t.validateAssignment(node, fromType, toType, "")
	}
}

// validateCallArguments checks function context compatibility for call arguments.
func (t *Transpiler) validateCallArguments(node *ast.Node) {
	if t.checker == nil {
		return
	}
	sig := t.checker.GetResolvedSignature(node)
	if sig == nil {
		return
	}
	params := checker.Signature_parameters(sig)

	var args *ast.NodeList
	switch node.Kind {
	case ast.KindCallExpression:
		args = node.AsCallExpression().Arguments
	case ast.KindNewExpression:
		args = node.AsNewExpression().Arguments
	default:
		return
	}
	if args == nil {
		return
	}
	if len(params) < len(args.Nodes) {
		return
	}
	for i, arg := range args.Nodes {
		if i >= len(params) {
			break
		}
		sigParam := params[i]
		if sigParam.ValueDeclaration == nil {
			continue
		}
		sigType := t.checker.GetTypeAtLocation(sigParam.ValueDeclaration)
		argType := t.checker.GetTypeAtLocation(arg)
		if sigType != nil && argType != nil {
			t.validateAssignment(arg, argType, sigType, sigParam.Name)
		}
	}
}

func (t *Transpiler) addUnsupportedNoSelfFunctionConversion(node *ast.Node, name string) {
	nameRef := ""
	if name != "" {
		nameRef = fmt.Sprintf(" '%s'", name)
	}
	t.addError(node, dw.UnsupportedNoSelfFunctionConversion,
		fmt.Sprintf("Unable to convert function with a 'this' parameter to function%s with no 'this'. "+
			"To fix, wrap in an arrow function, or declare with 'this: void'.", nameRef))
}

func (t *Transpiler) addUnsupportedSelfFunctionConversion(node *ast.Node, name string) {
	nameRef := ""
	if name != "" {
		nameRef = fmt.Sprintf(" '%s'", name)
	}
	t.addError(node, dw.UnsupportedSelfFunctionConversion,
		fmt.Sprintf("Unable to convert function with no 'this' parameter to function%s with 'this'. "+
			"To fix, wrap in an arrow function, or declare with 'this: any'.", nameRef))
}

func (t *Transpiler) addUnsupportedOverloadAssignment(node *ast.Node, name string) {
	nameRef := ""
	if name != "" {
		nameRef = fmt.Sprintf(" to '%s'", name)
	}
	t.addError(node, dw.UnsupportedOverloadAssignment,
		fmt.Sprintf("Unsupported assignment of function with different overloaded types for 'this'%s. "+
			"Overloads should all have the same type for 'this'.", nameRef))
}
