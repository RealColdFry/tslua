package transpiler

import (
	"fmt"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

// ExtensionKind identifies a TSTL language extension type.
type ExtensionKind string

const (
	ExtMultiFunction  ExtensionKind = "MultiFunction"
	ExtRangeFunction  ExtensionKind = "RangeFunction"
	ExtVarargConstant ExtensionKind = "VarargConstant"

	ExtAddition                 ExtensionKind = "Addition"
	ExtAdditionMethod           ExtensionKind = "AdditionMethod"
	ExtSubtraction              ExtensionKind = "Subtraction"
	ExtSubtractionMethod        ExtensionKind = "SubtractionMethod"
	ExtMultiplication           ExtensionKind = "Multiplication"
	ExtMultiplicationMethod     ExtensionKind = "MultiplicationMethod"
	ExtDivision                 ExtensionKind = "Division"
	ExtDivisionMethod           ExtensionKind = "DivisionMethod"
	ExtModulo                   ExtensionKind = "Modulo"
	ExtModuloMethod             ExtensionKind = "ModuloMethod"
	ExtPower                    ExtensionKind = "Power"
	ExtPowerMethod              ExtensionKind = "PowerMethod"
	ExtFloorDivision            ExtensionKind = "FloorDivision"
	ExtFloorDivisionMethod      ExtensionKind = "FloorDivisionMethod"
	ExtBitwiseAnd               ExtensionKind = "BitwiseAnd"
	ExtBitwiseAndMethod         ExtensionKind = "BitwiseAndMethod"
	ExtBitwiseOr                ExtensionKind = "BitwiseOr"
	ExtBitwiseOrMethod          ExtensionKind = "BitwiseOrMethod"
	ExtBitwiseExclusiveOr       ExtensionKind = "BitwiseExclusiveOr"
	ExtBitwiseExclusiveOrMethod ExtensionKind = "BitwiseExclusiveOrMethod"
	ExtBitwiseLeftShift         ExtensionKind = "BitwiseLeftShift"
	ExtBitwiseLeftShiftMethod   ExtensionKind = "BitwiseLeftShiftMethod"
	ExtBitwiseRightShift        ExtensionKind = "BitwiseRightShift"
	ExtBitwiseRightShiftMethod  ExtensionKind = "BitwiseRightShiftMethod"
	ExtConcat                   ExtensionKind = "Concat"
	ExtConcatMethod             ExtensionKind = "ConcatMethod"
	ExtLessThan                 ExtensionKind = "LessThan"
	ExtLessThanMethod           ExtensionKind = "LessThanMethod"
	ExtGreaterThan              ExtensionKind = "GreaterThan"
	ExtGreaterThanMethod        ExtensionKind = "GreaterThanMethod"
	ExtNegation                 ExtensionKind = "Negation"
	ExtNegationMethod           ExtensionKind = "NegationMethod"
	ExtBitwiseNot               ExtensionKind = "BitwiseNot"
	ExtBitwiseNotMethod         ExtensionKind = "BitwiseNotMethod"
	ExtLength                   ExtensionKind = "Length"
	ExtLengthMethod             ExtensionKind = "LengthMethod"

	ExtTableNew           ExtensionKind = "TableNew"
	ExtTableDelete        ExtensionKind = "TableDelete"
	ExtTableDeleteMethod  ExtensionKind = "TableDeleteMethod"
	ExtTableGet           ExtensionKind = "TableGet"
	ExtTableGetMethod     ExtensionKind = "TableGetMethod"
	ExtTableHas           ExtensionKind = "TableHas"
	ExtTableHasMethod     ExtensionKind = "TableHasMethod"
	ExtTableSet           ExtensionKind = "TableSet"
	ExtTableSetMethod     ExtensionKind = "TableSetMethod"
	ExtTableAddKey        ExtensionKind = "TableAddKey"
	ExtTableAddKeyMethod  ExtensionKind = "TableAddKeyMethod"
	ExtTableIsEmpty       ExtensionKind = "TableIsEmpty"
	ExtTableIsEmptyMethod ExtensionKind = "TableIsEmptyMethod"
)

var validExtensionKinds = map[string]ExtensionKind{
	"MultiFunction": ExtMultiFunction, "RangeFunction": ExtRangeFunction, "VarargConstant": ExtVarargConstant,
	"Addition": ExtAddition, "AdditionMethod": ExtAdditionMethod,
	"Subtraction": ExtSubtraction, "SubtractionMethod": ExtSubtractionMethod,
	"Multiplication": ExtMultiplication, "MultiplicationMethod": ExtMultiplicationMethod,
	"Division": ExtDivision, "DivisionMethod": ExtDivisionMethod,
	"Modulo": ExtModulo, "ModuloMethod": ExtModuloMethod,
	"Power": ExtPower, "PowerMethod": ExtPowerMethod,
	"FloorDivision": ExtFloorDivision, "FloorDivisionMethod": ExtFloorDivisionMethod,
	"BitwiseAnd": ExtBitwiseAnd, "BitwiseAndMethod": ExtBitwiseAndMethod,
	"BitwiseOr": ExtBitwiseOr, "BitwiseOrMethod": ExtBitwiseOrMethod,
	"BitwiseExclusiveOr": ExtBitwiseExclusiveOr, "BitwiseExclusiveOrMethod": ExtBitwiseExclusiveOrMethod,
	"BitwiseLeftShift": ExtBitwiseLeftShift, "BitwiseLeftShiftMethod": ExtBitwiseLeftShiftMethod,
	"BitwiseRightShift": ExtBitwiseRightShift, "BitwiseRightShiftMethod": ExtBitwiseRightShiftMethod,
	"Concat": ExtConcat, "ConcatMethod": ExtConcatMethod,
	"LessThan": ExtLessThan, "LessThanMethod": ExtLessThanMethod,
	"GreaterThan": ExtGreaterThan, "GreaterThanMethod": ExtGreaterThanMethod,
	"Negation": ExtNegation, "NegationMethod": ExtNegationMethod,
	"BitwiseNot": ExtBitwiseNot, "BitwiseNotMethod": ExtBitwiseNotMethod,
	"Length": ExtLength, "LengthMethod": ExtLengthMethod,
	"TableNew":    ExtTableNew,
	"TableDelete": ExtTableDelete, "TableDeleteMethod": ExtTableDeleteMethod,
	"TableGet": ExtTableGet, "TableGetMethod": ExtTableGetMethod,
	"TableHas": ExtTableHas, "TableHasMethod": ExtTableHasMethod,
	"TableSet": ExtTableSet, "TableSetMethod": ExtTableSetMethod,
	"TableAddKey": ExtTableAddKey, "TableAddKeyMethod": ExtTableAddKeyMethod,
	"TableIsEmpty": ExtTableIsEmpty, "TableIsEmptyMethod": ExtTableIsEmptyMethod,
}

func isMethodExtension(kind ExtensionKind) bool {
	switch kind {
	case ExtAdditionMethod, ExtSubtractionMethod, ExtMultiplicationMethod, ExtDivisionMethod,
		ExtModuloMethod, ExtPowerMethod, ExtFloorDivisionMethod,
		ExtBitwiseAndMethod, ExtBitwiseOrMethod, ExtBitwiseExclusiveOrMethod,
		ExtBitwiseLeftShiftMethod, ExtBitwiseRightShiftMethod,
		ExtConcatMethod, ExtLessThanMethod, ExtGreaterThanMethod,
		ExtNegationMethod, ExtBitwiseNotMethod, ExtLengthMethod,
		ExtTableDeleteMethod, ExtTableGetMethod, ExtTableHasMethod,
		ExtTableSetMethod, ExtTableAddKeyMethod, ExtTableIsEmptyMethod:
		return true
	}
	return false
}

// excludedTypeFlags filters out primitive and special types that cannot carry
// the __tstlExtension property. Matches TSTL's ((1 << 18) - 1) | Index | NonPrimitive.
const excludedTypeFlags = checker.TypeFlagsAny | checker.TypeFlagsUnknown |
	checker.TypeFlagsString | checker.TypeFlagsNumber | checker.TypeFlagsBoolean |
	checker.TypeFlagsEnum | checker.TypeFlagsBigInt |
	checker.TypeFlagsStringLiteral | checker.TypeFlagsNumberLiteral | checker.TypeFlagsBooleanLiteral |
	checker.TypeFlagsEnumLiteral | checker.TypeFlagsBigIntLiteral |
	checker.TypeFlagsESSymbol | checker.TypeFlagsUniqueESSymbol |
	checker.TypeFlagsVoid | checker.TypeFlagsUndefined | checker.TypeFlagsNull |
	checker.TypeFlagsNever |
	checker.TypeFlagsIndex | checker.TypeFlagsNonPrimitive

// getPropertyStringValue reads a string literal property value from a type.
// Returns "" if the property doesn't exist or isn't a string literal.
func (t *Transpiler) getPropertyStringValue(typ *checker.Type, propertyName string) string {
	flags := checker.Type_flags(typ)
	if flags&excludedTypeFlags != 0 {
		return ""
	}
	prop := checker.Checker_getPropertyOfType(t.checker, typ, propertyName)
	if prop == nil {
		return ""
	}
	propType := checker.Checker_getTypeOfSymbol(t.checker, prop)
	if propType == nil {
		return ""
	}
	if !propType.IsStringLiteral() {
		return ""
	}
	if v, ok := propType.AsLiteralType().Value().(string); ok {
		return v
	}
	return ""
}

// getExtensionKindForType checks whether a type carries a __tstlExtension property
// with a valid extension kind value.
func (t *Transpiler) getExtensionKindForType(typ *checker.Type) (ExtensionKind, bool) {
	value := t.getPropertyStringValue(typ, "__tstlExtension")
	if value == "" {
		return "", false
	}
	if kind, ok := validExtensionKinds[value]; ok {
		return kind, true
	}
	return "", false
}

// getExtensionKindForSymbol resolves the extension kind of a symbol by examining its type.
func (t *Transpiler) getExtensionKindForSymbol(sym *ast.Symbol) (ExtensionKind, bool) {
	if sym == nil {
		return "", false
	}
	typ := checker.Checker_getTypeOfSymbol(t.checker, sym)
	if typ == nil {
		return "", false
	}
	return t.getExtensionKindForType(typ)
}

// getExtensionKindForNode resolves the extension kind of a node by examining its type.
func (t *Transpiler) getExtensionKindForNode(node *ast.Node) (ExtensionKind, bool) {

	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return "", false
	}
	if ast.IsOptionalChain(node) {
		typ = checker.Checker_GetNonNullableType(t.checker, typ)
	}
	return t.getExtensionKindForType(typ)
}

// ==========================================================================
// Extension argument extraction
// ==========================================================================

// getNaryCallExtensionArgs extracts arguments from an extension call, handling
// both function form (ext(a,b,c)) and method form (a.ext(b,c)).
func (t *Transpiler) getNaryCallExtensionArgs(ce *ast.CallExpression, kind ExtensionKind, numArgs int) []*ast.Node {
	// Spread elements are invalid in extension calls
	for _, arg := range ce.Arguments.Nodes {
		if arg.Kind == ast.KindSpreadElement {
			t.addError(arg, dw.InvalidSpreadInCallExtension, "Spread elements are not supported in language extension calls")
			return nil
		}
	}

	if isMethodExtension(kind) {
		expr := ce.Expression
		if expr.Kind != ast.KindPropertyAccessExpression && expr.Kind != ast.KindElementAccessExpression {
			t.addError(ce.Expression, dw.InvalidMethodCallExtensionUse, "Language extension method must be called as a method (obj.method())")
			return nil
		}
		if len(ce.Arguments.Nodes) < numArgs-1 {
			return nil // TS error
		}
		// For method form, extract the receiver from the callee expression
		var receiver *ast.Node
		if expr.Kind == ast.KindPropertyAccessExpression {
			receiver = expr.AsPropertyAccessExpression().Expression
		} else {
			receiver = expr.AsElementAccessExpression().Expression
		}
		result := make([]*ast.Node, 0, 1+len(ce.Arguments.Nodes))
		result = append(result, receiver)
		result = append(result, ce.Arguments.Nodes...)
		return result
	}

	if len(ce.Arguments.Nodes) < numArgs {
		return nil // TS error
	}
	return ce.Arguments.Nodes
}

func (t *Transpiler) getUnaryCallExtensionArg(ce *ast.CallExpression, kind ExtensionKind) *ast.Node {
	args := t.getNaryCallExtensionArgs(ce, kind, 1)
	if args == nil {
		return nil
	}
	return args[0]
}

func (t *Transpiler) getBinaryCallExtensionArgs(ce *ast.CallExpression, kind ExtensionKind) (*ast.Node, *ast.Node) {
	args := t.getNaryCallExtensionArgs(ce, kind, 2)
	if args == nil {
		return nil, nil
	}
	return args[0], args[1]
}

// ==========================================================================
// Extension call dispatch
// ==========================================================================

// tryTransformLanguageExtensionCallExpression checks if a call expression is a
// language extension call and transforms it accordingly. Returns nil if not an extension.
func (t *Transpiler) tryTransformLanguageExtensionCallExpression(node *ast.Node) lua.Expression {
	ce := node.AsCallExpression()
	kind, ok := t.getExtensionKindForNode(ce.Expression)
	if !ok {
		return nil
	}
	t.checkOperatorRequiresLua53(node, kind)

	switch kind {
	// Table operations
	case ExtTableDelete, ExtTableDeleteMethod:
		return t.transformTableDeleteExpression(ce, kind)
	case ExtTableGet, ExtTableGetMethod:
		return t.transformTableGetExpression(ce, kind)
	case ExtTableHas, ExtTableHasMethod:
		return t.transformTableHasExpression(ce, kind)
	case ExtTableSet, ExtTableSetMethod:
		return t.transformTableSetExpression(ce, kind)
	case ExtTableAddKey, ExtTableAddKeyMethod:
		return t.transformTableAddKeyExpression(ce, kind)
	case ExtTableIsEmpty, ExtTableIsEmptyMethod:
		return t.transformTableIsEmptyExpression(ce, kind)

	// Operator extensions
	case ExtLength, ExtLengthMethod:
		arg := t.getUnaryCallExtensionArg(ce, kind)
		if arg == nil {
			return lua.Nil()
		}
		return lua.Unary(lua.OpLen, t.transformExpression(arg))
	case ExtNegation, ExtNegationMethod:
		arg := t.getUnaryCallExtensionArg(ce, kind)
		if arg == nil {
			return lua.Nil()
		}
		return lua.Unary(lua.OpNeg, t.transformExpression(arg))
	case ExtBitwiseNot, ExtBitwiseNotMethod:
		arg := t.getUnaryCallExtensionArg(ce, kind)
		if arg == nil {
			return lua.Nil()
		}
		return lua.Unary(lua.OpBitNot, t.transformExpression(arg))
	case ExtAddition, ExtAdditionMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpAdd)
	case ExtSubtraction, ExtSubtractionMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpSub)
	case ExtMultiplication, ExtMultiplicationMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpMul)
	case ExtDivision, ExtDivisionMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpDiv)
	case ExtModulo, ExtModuloMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpMod)
	case ExtPower, ExtPowerMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpPow)
	case ExtFloorDivision, ExtFloorDivisionMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpFloorDiv)
	case ExtBitwiseAnd, ExtBitwiseAndMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpBitAnd)
	case ExtBitwiseOr, ExtBitwiseOrMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpBitOr)
	case ExtBitwiseExclusiveOr, ExtBitwiseExclusiveOrMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpBitXor)
	case ExtBitwiseLeftShift, ExtBitwiseLeftShiftMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpBitShl)
	case ExtBitwiseRightShift, ExtBitwiseRightShiftMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpBitShr)
	case ExtConcat, ExtConcatMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpConcat)
	case ExtLessThan, ExtLessThanMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpLt)
	case ExtGreaterThan, ExtGreaterThanMethod:
		return t.transformBinaryOperatorExtension(ce, kind, lua.OpGt)
	}
	return nil
}

// transformBinaryOperatorExtension handles binary operator extensions (add, sub, etc.).
func (t *Transpiler) transformBinaryOperatorExtension(ce *ast.CallExpression, kind ExtensionKind, op lua.Operator) lua.Expression {
	leftNode, rightNode := t.getBinaryCallExtensionArgs(ce, kind)
	if leftNode == nil {
		return lua.Nil()
	}
	exprs := t.transformOrderedExpressions([]*ast.Node{leftNode, rightNode})
	return lua.Binary(exprs[0], op, exprs[1])
}

// checkOperatorRequiresLua53 emits a diagnostic if a Lua 5.3+ operator extension is used with a pre-5.3 target.
func (t *Transpiler) checkOperatorRequiresLua53(node *ast.Node, kind ExtensionKind) {
	switch kind {
	case ExtFloorDivision, ExtFloorDivisionMethod,
		ExtBitwiseAnd, ExtBitwiseAndMethod,
		ExtBitwiseOr, ExtBitwiseOrMethod,
		ExtBitwiseExclusiveOr, ExtBitwiseExclusiveOrMethod,
		ExtBitwiseLeftShift, ExtBitwiseLeftShiftMethod,
		ExtBitwiseRightShift, ExtBitwiseRightShiftMethod,
		ExtBitwiseNot, ExtBitwiseNotMethod:
		// requires Lua 5.3+
	default:
		return
	}
	if t.luaTarget.HasNativeBitwise() {
		return
	}
	luaTarget := t.luaTarget
	if luaTarget == LuaTargetUniversal {
		luaTarget = LuaTargetLua51
	}
	switch kind {
	case ExtFloorDivision, ExtFloorDivisionMethod:
		t.addError(node, dw.UnsupportedForTarget, fmt.Sprintf("Floor division operator is/are not supported for target %s.", luaTarget.DisplayName()))
	default:
		t.addError(node, dw.UnsupportedForTarget, fmt.Sprintf("Native bitwise operations is/are not supported for target %s.", luaTarget.DisplayName()))
	}
}

// isTableNewCall checks if a new expression instantiates a LuaTable extension type.
func (t *Transpiler) isTableNewCall(ne *ast.NewExpression) bool {
	kind, ok := t.getExtensionKindForNode(ne.Expression)
	return ok && kind == ExtTableNew
}

// ==========================================================================
// $range transform
// ==========================================================================

// isRangeCall checks if a call expression is a $range() call.
func (t *Transpiler) isRangeCall(expr *ast.Node) bool {
	if expr.Kind != ast.KindCallExpression {
		return false
	}
	ce := expr.AsCallExpression()
	if ce.Expression.Kind != ast.KindIdentifier {
		return false
	}
	kind, ok := t.getExtensionKindForNode(ce.Expression)
	return ok && kind == ExtRangeFunction
}

// tryTransformRangeForOf transforms `for (const i of $range(start, limit, step))` into
// a Lua numeric for loop: `for i = start, limit, step do`.
// Returns nil if the expression is not a $range call.
func (t *Transpiler) tryTransformRangeForOf(node *ast.Node, fs *ast.ForInOrOfStatement) []lua.Statement {
	if !t.isRangeCall(fs.Expression) {
		return nil
	}
	ce := fs.Expression.AsCallExpression()

	// Control variable must be a simple variable declaration (not destructuring, not existing var)
	var controlVar string
	init := fs.Initializer
	if init.Kind == ast.KindVariableDeclarationList {
		declList := init.AsVariableDeclarationList()
		if len(declList.Declarations.Nodes) > 0 {
			name := declList.Declarations.Nodes[0].AsVariableDeclaration().Name()
			if name.Kind == ast.KindIdentifier {
				controlVar = identName(t.transformExpression(name))
			}
		}
	}
	if controlVar == "" {
		t.addError(init, dw.InvalidRangeControlVariable, "For loop using $range must declare a single control variable.")
		controlVar = "____"
	}

	// Transform arguments: $range(start, limit[, step])
	args := ce.Arguments.Nodes
	var startExpr, limitExpr, stepExpr lua.Expression
	switch len(args) {
	case 0:
		startExpr = lua.Num("0")
		limitExpr = lua.Num("0")
	case 1:
		startExpr = t.transformExpression(args[0])
		limitExpr = lua.Num("0")
	case 2:
		exprs := t.transformOrderedExpressions(args[:2])
		startExpr = exprs[0]
		limitExpr = exprs[1]
	default:
		exprs := t.transformOrderedExpressions(args[:3])
		startExpr = exprs[0]
		limitExpr = exprs[1]
		stepExpr = exprs[2]
	}

	bodyStmts := t.transformLoopBody(fs.Statement)

	return []lua.Statement{&lua.ForStatement{
		ControlVariable:            lua.Ident(controlVar),
		ControlVariableInitializer: startExpr,
		LimitExpression:            limitExpr,
		StepExpression:             stepExpr,
		Body:                       &lua.Block{Statements: bodyStmts},
	}}
}

// ==========================================================================
// Table extension transforms
// ==========================================================================

// transformTableDeleteExpression: table.delete(key) → table[key] = nil; return true
func (t *Transpiler) transformTableDeleteExpression(ce *ast.CallExpression, kind ExtensionKind) lua.Expression {
	tableNode, keyNode := t.getBinaryCallExtensionArgs(ce, kind)
	if tableNode == nil {
		return lua.Nil()
	}
	exprs := t.transformOrderedExpressions([]*ast.Node{tableNode, keyNode})
	t.addPrecedingStatements(lua.Assign(
		[]lua.Expression{lua.Index(exprs[0], exprs[1])},
		[]lua.Expression{lua.Nil()},
	))
	return lua.Bool(true)
}

// transformTableGetExpression: table.get(key) → table[key]
func (t *Transpiler) transformTableGetExpression(ce *ast.CallExpression, kind ExtensionKind) lua.Expression {
	tableNode, keyNode := t.getBinaryCallExtensionArgs(ce, kind)
	if tableNode == nil {
		return lua.Nil()
	}
	exprs := t.transformOrderedExpressions([]*ast.Node{tableNode, keyNode})
	return lua.Index(exprs[0], exprs[1])
}

// transformTableHasExpression: table.has(key) → table[key] ~= nil
func (t *Transpiler) transformTableHasExpression(ce *ast.CallExpression, kind ExtensionKind) lua.Expression {
	tableNode, keyNode := t.getBinaryCallExtensionArgs(ce, kind)
	if tableNode == nil {
		return lua.Nil()
	}
	exprs := t.transformOrderedExpressions([]*ast.Node{tableNode, keyNode})
	return lua.Binary(lua.Index(exprs[0], exprs[1]), lua.OpNeq, lua.Nil())
}

// transformTableSetExpression: table.set(key, value) → table[key] = value
func (t *Transpiler) transformTableSetExpression(ce *ast.CallExpression, kind ExtensionKind) lua.Expression {
	args := t.getNaryCallExtensionArgs(ce, kind, 3)
	if args == nil {
		return lua.Nil()
	}
	exprs := t.transformOrderedExpressions(args[:3])
	t.addPrecedingStatements(lua.Assign(
		[]lua.Expression{lua.Index(exprs[0], exprs[1])},
		[]lua.Expression{exprs[2]},
	))
	return lua.Nil()
}

// transformTableAddKeyExpression: table.addKey(key) → table[key] = true
func (t *Transpiler) transformTableAddKeyExpression(ce *ast.CallExpression, kind ExtensionKind) lua.Expression {
	tableNode, keyNode := t.getBinaryCallExtensionArgs(ce, kind)
	if tableNode == nil {
		return lua.Nil()
	}
	exprs := t.transformOrderedExpressions([]*ast.Node{tableNode, keyNode})
	t.addPrecedingStatements(lua.Assign(
		[]lua.Expression{lua.Index(exprs[0], exprs[1])},
		[]lua.Expression{lua.Bool(true)},
	))
	return lua.Nil()
}

// transformTableIsEmptyExpression: table.isEmpty() → next(table) == nil
func (t *Transpiler) transformTableIsEmptyExpression(ce *ast.CallExpression, kind ExtensionKind) lua.Expression {
	tableNode := t.getUnaryCallExtensionArg(ce, kind)
	if tableNode == nil {
		return lua.Nil()
	}
	tableExpr := t.transformExpression(tableNode)
	return lua.Binary(lua.Call(lua.Ident("next"), tableExpr), lua.OpEq, lua.Nil())
}

// ==========================================================================
// Identifier extension validation
// ==========================================================================

// checkExtensionIdentifier checks if an identifier is a language extension used outside
// its valid context. Returns a replacement expression if the identifier was handled
// (e.g., anonymous ident for invalid extension values with type info), or nil to let
// normal identifier processing continue.
func (t *Transpiler) checkExtensionIdentifier(node *ast.Node, text string) lua.Expression {
	var extKind ExtensionKind
	var hasExtKind bool
	if sym := t.checker.GetSymbolAtLocation(node); sym != nil {
		extKind, hasExtKind = t.getExtensionKindForSymbol(sym)
	} else {
		extKind, hasExtKind = t.getExtensionKindForNode(node)
	}
	// Fallback: detect $multi/$range/$vararg by name when type info is unavailable
	// (e.g., language-extensions types not loaded). Emit the extension-specific
	// diagnostic and return a placeholder so the generic invalidAmbientIdentifierName
	// diagnostic doesn't fire on top of it.
	if !hasExtKind {
		if nameKind, ok := extensionKindByName(text); ok {
			if t.isExtensionValueIdentifier(node, nameKind) {
				t.reportInvalidExtensionValue(node, nameKind)
				return lua.Ident("____")
			}
		}
		return nil
	}

	if t.isCallExtensionKind(extKind) {
		// Call extensions (operators, table ops) used as standalone identifiers are invalid,
		// unless this is the name of a variable declaration (inferred type carries extension kind).
		if !(node.Parent.Kind == ast.KindVariableDeclaration &&
			node.Parent.AsVariableDeclaration().Name() == node) {
			t.addError(node, dw.InvalidCallExtensionUse, "This function must be called directly and cannot be referred to.")
		}
		return nil
	}

	if t.isExtensionValueIdentifier(node, extKind) {
		t.reportInvalidExtensionValue(node, extKind)
		return lua.Ident("____")
	}

	return nil
}

// resolveIdentifierName applies Lua-safe renaming to an identifier.
func (t *Transpiler) resolveIdentifierName(node *ast.Node, text string) string {
	if t.hasUnsafeIdentifierName(node) {
		return luaSafeName(text)
	}
	return text
}

// extensionKindByName returns the extension kind for a $-prefixed identifier name.
// Used as a fallback when type information is unavailable (e.g., without language-extensions types).
func extensionKindByName(name string) (ExtensionKind, bool) {
	switch name {
	case "$multi":
		return ExtMultiFunction, true
	case "$range":
		return ExtRangeFunction, true
	case "$vararg":
		return ExtVarargConstant, true
	}
	return "", false
}

// extensionKindToValueName maps extension kinds to their corresponding $-prefixed identifiers.
var extensionKindToValueName = map[ExtensionKind]string{
	ExtMultiFunction:  "$multi",
	ExtRangeFunction:  "$range",
	ExtVarargConstant: "$vararg",
}

// isExtensionValueIdentifier checks if an identifier's symbol name matches the
// expected $-prefixed name for its extension kind.
func (t *Transpiler) isExtensionValueIdentifier(node *ast.Node, kind ExtensionKind) bool {
	expectedName, ok := extensionKindToValueName[kind]
	if !ok {
		return false
	}
	name := node.AsIdentifier().Text

	sym := t.checker.GetSymbolAtLocation(node)
	if sym != nil {
		return sym.Name == expectedName
	}
	// No symbol found (e.g., language extension types not loaded) — match by identifier name
	return name == expectedName
}

// reportInvalidExtensionValue emits diagnostics for language extension values used outside their valid context.
func (t *Transpiler) reportInvalidExtensionValue(node *ast.Node, kind ExtensionKind) {
	switch kind {
	case ExtMultiFunction:
		t.addError(node, dw.InvalidMultiFunctionUse, "The $multi function must be called in a return statement.")
	case ExtRangeFunction:
		t.addError(node, dw.InvalidRangeUse, "$range can only be used in a for...of loop.")
	case ExtVarargConstant:
		t.addError(node, dw.InvalidVarargUse, "$vararg can only be used in a spread element ('...$vararg') in global scope.")
	}
}

// isCallExtensionKind returns true if the extension kind represents a call extension
// (operators, table operations) that should only be used as call expressions.
func (t *Transpiler) isCallExtensionKind(kind ExtensionKind) bool {
	switch kind {
	case ExtAddition, ExtAdditionMethod, ExtSubtraction, ExtSubtractionMethod,
		ExtMultiplication, ExtMultiplicationMethod, ExtDivision, ExtDivisionMethod,
		ExtModulo, ExtModuloMethod, ExtPower, ExtPowerMethod,
		ExtFloorDivision, ExtFloorDivisionMethod,
		ExtBitwiseAnd, ExtBitwiseAndMethod, ExtBitwiseOr, ExtBitwiseOrMethod,
		ExtBitwiseExclusiveOr, ExtBitwiseExclusiveOrMethod,
		ExtBitwiseLeftShift, ExtBitwiseLeftShiftMethod,
		ExtBitwiseRightShift, ExtBitwiseRightShiftMethod,
		ExtConcat, ExtConcatMethod, ExtLessThan, ExtLessThanMethod,
		ExtGreaterThan, ExtGreaterThanMethod,
		ExtNegation, ExtNegationMethod, ExtBitwiseNot, ExtBitwiseNotMethod,
		ExtLength, ExtLengthMethod,
		ExtTableNew, ExtTableDelete, ExtTableDeleteMethod,
		ExtTableGet, ExtTableGetMethod, ExtTableHas, ExtTableHasMethod,
		ExtTableSet, ExtTableSetMethod, ExtTableAddKey, ExtTableAddKeyMethod,
		ExtTableIsEmpty, ExtTableIsEmptyMethod:
		return true
	}
	return false
}

// ==========================================================================
// Multi-return helpers
// ==========================================================================

// isMultiReturnType checks if a type has the __tstlMultiReturn marker property.
// Unlike __tstlExtension (which is a string literal), __tstlMultiReturn is typed as `any`,
// so we just check for property existence.
func (t *Transpiler) isMultiReturnType(typ *checker.Type) bool {
	if typ == nil {
		return false
	}
	flags := checker.Type_flags(typ)
	if flags&excludedTypeFlags != 0 {
		return false
	}
	return checker.Checker_getPropertyOfType(t.checker, typ, "__tstlMultiReturn") != nil
}

// canBeMultiReturnType checks if a type can be a LuaMultiReturn (is any, is multi-return, or union containing one).
func (t *Transpiler) canBeMultiReturnType(typ *checker.Type) bool {
	if typ == nil {
		return false
	}
	flags := checker.Type_flags(typ)
	if flags&checker.TypeFlagsAny != 0 {
		return true
	}
	if t.isMultiReturnType(typ) {
		return true
	}
	if flags&checker.TypeFlagsUnion != 0 {
		for _, member := range typ.Types() {
			if t.canBeMultiReturnType(member) {
				return true
			}
		}
	}
	return false
}

// isMultiFunctionCall checks if a call expression is a direct $multi() call.
func (t *Transpiler) isMultiFunctionCall(ce *ast.CallExpression) bool {
	kind, ok := t.getExtensionKindForNode(ce.Expression)
	return ok && kind == ExtMultiFunction
}

// returnsMultiType checks if a call expression returns a LuaMultiReturn type.
func (t *Transpiler) returnsMultiType(node *ast.Node) bool {

	if node.Kind != ast.KindCallExpression {
		return false
	}
	sig := t.checker.GetResolvedSignature(node)
	if sig == nil {
		return false
	}
	retType := t.checker.GetReturnTypeOfSignature(sig)
	if retType == nil {
		return false
	}
	return t.isMultiReturnType(retType)
}

// isMultiReturnCall checks if an expression is a call that returns a multi-return type.
func (t *Transpiler) isMultiReturnCall(node *ast.Node) bool {
	return node.Kind == ast.KindCallExpression && t.returnsMultiType(node)
}

// isInMultiReturnFunction checks if a node is inside a function that returns a multi-return type.
func (t *Transpiler) isInMultiReturnFunction(node *ast.Node) bool {

	current := node.Parent
	for current != nil {
		switch current.Kind {
		case ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindFunctionExpression,
			ast.KindArrowFunction, ast.KindGetAccessor:
			sig := t.checker.GetSignatureFromDeclaration(current)
			if sig == nil {
				return false
			}
			retType := t.checker.GetReturnTypeOfSignature(sig)
			if retType == nil {
				return false
			}
			return t.isMultiReturnType(retType)
		}
		current = current.Parent
	}
	return false
}

// shouldMultiReturnCallBeWrapped determines if a multi-return call needs to be
// wrapped in parentheses to extract only the first value.
func (t *Transpiler) shouldMultiReturnCallBeWrapped(node *ast.Node) bool {
	if !t.returnsMultiType(node) {
		return false
	}
	parent := skipOuterExpressions(node.Parent)

	// Variable declaration with destructuring
	if parent.Kind == ast.KindVariableDeclaration {
		name := parent.AsVariableDeclaration().Name()
		if name.Kind == ast.KindArrayBindingPattern {
			return false
		}
	}

	// Assignment with destructuring
	if parent.Kind == ast.KindBinaryExpression {
		be := parent.AsBinaryExpression()
		if be.OperatorToken.Kind == ast.KindEqualsToken && be.Left.Kind == ast.KindArrayLiteralExpression {
			return false
		}
	}

	// Spread operator
	if parent.Kind == ast.KindSpreadElement {
		return false
	}

	// Stand-alone expression statement
	if parent.Kind == ast.KindExpressionStatement {
		return false
	}

	// Forwarded multi-return: return multiCall() in a multi-return function
	if (parent.Kind == ast.KindReturnStatement || parent.Kind == ast.KindArrowFunction) &&
		t.isInMultiReturnFunction(node) {
		return false
	}

	// Element access: foo()[0] will use select
	if parent.Kind == ast.KindElementAccessExpression {
		return false
	}

	// for...of with LuaIterable
	if parent.Kind == ast.KindForOfStatement {
		if _, ok := t.getIterableExtensionKindForNode(node); ok {
			return false
		}
	}

	return true
}

// skipOuterExpressions unwraps type assertions and parenthesized expressions
// to find the inner expression. Matches TSTL's ts.skipOuterExpressions with Assertions flag.
func skipOuterExpressions(node *ast.Node) *ast.Node {
	for node != nil {
		switch node.Kind {
		case ast.KindAsExpression:
			node = node.AsAsExpression().Expression
		case ast.KindTypeAssertionExpression:
			node = node.AsTypeAssertion().Expression
		case ast.KindNonNullExpression:
			node = node.AsNonNullExpression().Expression
		case ast.KindSatisfiesExpression:
			node = node.AsSatisfiesExpression().Expression
		default:
			return node
		}
	}
	return node
}

// skipOuterExpressionsDown unwraps assertions going into an expression (downward).
func skipOuterExpressionsDown(node *ast.Node) *ast.Node {
	for node != nil {
		switch node.Kind {
		case ast.KindAsExpression:
			node = node.AsAsExpression().Expression
		case ast.KindTypeAssertionExpression:
			node = node.AsTypeAssertion().Expression
		case ast.KindNonNullExpression:
			node = node.AsNonNullExpression().Expression
		case ast.KindParenthesizedExpression:
			node = node.AsParenthesizedExpression().Expression
		case ast.KindSatisfiesExpression:
			node = node.AsSatisfiesExpression().Expression
		default:
			return node
		}
	}
	return node
}

// transformExpressionsInReturn handles multi-return in return position.
// Returns multiple expressions for `return $multi(a, b)` → `return a, b`.
func (t *Transpiler) transformExpressionsInReturn(expr *ast.Node) []lua.Expression {
	inner := skipOuterExpressionsDown(expr)

	if inner.Kind == ast.KindCallExpression {
		ce := inner.AsCallExpression()
		// return $multi(a, b, c) → return a, b, c
		if t.isMultiFunctionCall(ce) {
			// Don't allow $multi to be implicitly cast to something other than LuaMultiReturn
			if ctxType := checker.Checker_getContextualType(t.checker, expr, checker.ContextFlagsNone); ctxType != nil && !t.canBeMultiReturnType(ctxType) {
				t.addError(inner, dw.InvalidMultiFunctionReturnType, "The $multi function cannot be cast to a non-LuaMultiReturn type.")
			}
			return t.transformArgExprs(ce.Arguments)
		}
	}

	// Unpack variables/expressions typed as LuaMultiReturn when inside a multi-return function.
	// e.g. `const m = foo(); return m;` where m is LuaMultiReturn → `return unpack(m)`
	if t.isInMultiReturnFunction(expr) {
		exprType := t.checker.GetTypeAtLocation(inner)
		if exprType != nil && t.isMultiReturnType(exprType) && inner.Kind != ast.KindCallExpression {
			luaExpr := t.transformExpression(expr)
			return []lua.Expression{lua.Call(t.unpackIdent(), luaExpr)}
		}
	}

	return []lua.Expression{t.transformExpression(expr)}
}

// ==========================================================================
// Iterable extension transforms
// ==========================================================================

// tryTransformIterableForOf handles for...of on LuaIterable/LuaPairsIterable/LuaPairsKeyIterable.
// Returns nil if the expression is not an iterable extension type.
func (t *Transpiler) tryTransformIterableForOf(node *ast.Node, fs *ast.ForInOrOfStatement) []lua.Statement {

	typ := t.checker.GetTypeAtLocation(fs.Expression)
	if typ == nil {
		return nil
	}
	kind, ok := t.getIterableExtensionKindForType(typ)
	if !ok {
		return nil
	}

	iterExpr := t.transformExpression(fs.Expression)
	loopBodyStmts := t.transformLoopBody(fs.Statement)

	switch kind {
	case IterableKindIterable:
		// Check if the iterable value type is LuaMultiReturn — if so, use multi-variable for-in
		if t.isMultiReturnIterable(fs.Expression) {
			names, bodyPreamble := t.extractMultiValueForOfNames(fs.Initializer, dw.InvalidMultiIterableWithoutDestructuring, "LuaIterable with a LuaMultiReturn return value type must be destructured.")
			var bodyStmts []lua.Statement
			bodyStmts = append(bodyStmts, bodyPreamble...)
			bodyStmts = append(bodyStmts, loopBodyStmts...)
			return []lua.Statement{lua.ForIn(
				names,
				[]lua.Expression{iterExpr},
				&lua.Block{Statements: bodyStmts},
			)}
		}
		// Single-value iterable
		varName, varPrec, bodyPreamble := t.extractForOfInitializer(fs.Initializer)
		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, bodyPreamble...)
		bodyStmts = append(bodyStmts, loopBodyStmts...)
		result := make([]lua.Statement, 0, len(varPrec)+1)
		result = append(result, varPrec...)
		result = append(result, lua.ForIn(
			[]*lua.Identifier{lua.Ident(varName)},
			[]lua.Expression{iterExpr},
			&lua.Block{Statements: bodyStmts},
		))
		return result

	case IterableKindPairs:
		// for...of on LuaPairsIterable: for k, v in pairs(table) do
		// Initializer must be destructuring [k, v]
		names, pairsPreamble := t.extractMultiValueForOfNames(fs.Initializer, dw.InvalidPairsIterableWithoutDestructuring, "LuaPairsIterable type must be destructured in a for...of statement.")
		var pairsBody []lua.Statement
		pairsBody = append(pairsBody, pairsPreamble...)
		pairsBody = append(pairsBody, loopBodyStmts...)
		return []lua.Statement{lua.ForIn(
			names,
			[]lua.Expression{lua.Call(lua.Ident("pairs"), iterExpr)},
			&lua.Block{Statements: pairsBody},
		)}

	case IterableKindPairsKey:
		// for...of on LuaPairsKeyIterable: for k in pairs(table) do
		varName, varPrec, bodyPreamble := t.extractForOfInitializer(fs.Initializer)
		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, bodyPreamble...)
		bodyStmts = append(bodyStmts, loopBodyStmts...)
		result := make([]lua.Statement, 0, len(varPrec)+1)
		result = append(result, varPrec...)
		result = append(result, lua.ForIn(
			[]*lua.Identifier{lua.Ident(varName)},
			[]lua.Expression{lua.Call(lua.Ident("pairs"), iterExpr)},
			&lua.Block{Statements: bodyStmts},
		))
		return result
	}

	return nil
}

// isMultiReturnIterable checks if an iterable expression's value type is LuaMultiReturn.
func (t *Transpiler) isMultiReturnIterable(expr *ast.Node) bool {

	typ := t.checker.GetTypeAtLocation(expr)
	if typ == nil {
		return false
	}
	// Get type arguments from the LuaIterable type.
	// LuaIterable<T> where T might be LuaMultiReturn<[...]>
	if typeArgs := safeGetTypeArguments(t.checker, typ); len(typeArgs) > 0 && t.isMultiReturnType(typeArgs[0]) {
		return true
	}
	// For intersection types, find the Iterable member
	flags := checker.Type_flags(typ)
	if flags&checker.TypeFlagsIntersection != 0 {
		for _, member := range typ.Types() {
			if memberArgs := safeGetTypeArguments(t.checker, member); len(memberArgs) > 0 && t.isMultiReturnType(memberArgs[0]) {
				return true
			}
		}
	}
	return false
}

// safeGetTypeArguments wraps Checker_getTypeArguments with a recover since it
// panics on types that aren't type references (e.g. function types).
func safeGetTypeArguments(ch *checker.Checker, typ *checker.Type) (result []*checker.Type) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
		}
	}()
	// Only call for object types with Reference flag (type references have type arguments)
	objFlags := checker.Type_objectFlags(typ)
	if objFlags&checker.ObjectFlagsReference == 0 {
		return nil
	}
	return checker.Checker_getTypeArguments(ch, typ)
}

// extractMultiValueForOfNames extracts multiple variable names from a for-of
// initializer that uses destructuring. Returns the loop variable names and
// any preamble statements needed for complex destructuring patterns.
// Handles both: `for (const [k, v] of ...)` (VariableDeclarationList)
// and: `for ([x, y] of ...)` (ArrayLiteralExpression with existing vars)
func (t *Transpiler) extractMultiValueForOfNames(init *ast.Node, invalidCode dw.DiagCode, invalidMsg string) ([]*lua.Identifier, []lua.Statement) {
	if init.Kind == ast.KindVariableDeclarationList {
		declList := init.AsVariableDeclarationList()
		if len(declList.Declarations.Nodes) > 0 {
			name := declList.Declarations.Nodes[0].AsVariableDeclaration().Name()
			if name.Kind == ast.KindArrayBindingPattern {
				bp := name.AsBindingPattern()
				var idents []*lua.Identifier
				var preamble []lua.Statement
				for _, elem := range bp.Elements.Nodes {
					if elem.Kind == ast.KindOmittedExpression {
						idents = append(idents, lua.Ident("_"))
						continue
					}
					be := elem.AsBindingElement()
					if be.Name() != nil && be.Name().Kind == ast.KindIdentifier {
						idents = append(idents, lua.Ident(identName(t.transformExpression(be.Name()))))
					} else if be.Name() != nil && (be.Name().Kind == ast.KindObjectBindingPattern || be.Name().Kind == ast.KindArrayBindingPattern) {
						// Complex pattern: use temp variable and destructure in loop body
						temp := t.nextTemp("temp")
						idents = append(idents, lua.Ident(temp))
						preamble = append(preamble, t.transformBindingPattern(be.Name(), lua.Ident(temp), true, false)...)
					} else {
						idents = append(idents, lua.Ident("_"))
					}
				}
				if len(idents) > 0 {
					return idents, preamble
				}
			} else {
				// Not destructuring — emit diagnostic
				t.addError(name, invalidCode, invalidMsg)
			}
		}
	} else if init.Kind == ast.KindArrayLiteralExpression {
		// Existing variable destructuring: for ([x, y] of ...)
		al := init.AsArrayLiteralExpression()
		var idents []*lua.Identifier
		for _, elem := range al.Elements.Nodes {
			if elem.Kind == ast.KindOmittedExpression {
				idents = append(idents, lua.Ident("_"))
			} else {
				idents = append(idents, lua.Ident(identName(t.transformExpression(elem))))
			}
		}
		if len(idents) > 0 {
			return idents, nil
		}
	} else {
		// Not destructuring — emit diagnostic
		t.addError(init, invalidCode, invalidMsg)
	}
	// Fallback: single anonymous variable
	return []*lua.Identifier{lua.Ident("____")}, nil
}

// ==========================================================================
// Iterable extension helpers
// ==========================================================================

type IterableExtensionKind string

const (
	IterableKindIterable IterableExtensionKind = "Iterable"
	IterableKindPairs    IterableExtensionKind = "Pairs"
	IterableKindPairsKey IterableExtensionKind = "PairsKey"
)

var validIterableKinds = map[string]IterableExtensionKind{
	"Iterable": IterableKindIterable,
	"Pairs":    IterableKindPairs,
	"PairsKey": IterableKindPairsKey,
}

// getIterableExtensionKindForType returns the iterable extension kind for a type.
func (t *Transpiler) getIterableExtensionKindForType(typ *checker.Type) (IterableExtensionKind, bool) {
	if typ == nil {
		return "", false
	}
	value := t.getPropertyStringValue(typ, "__tstlIterable")
	if value == "" {
		return "", false
	}
	if kind, ok := validIterableKinds[value]; ok {
		return kind, true
	}
	return "", false
}

// getIterableExtensionKindForNode returns the iterable extension kind from a node's type.
func (t *Transpiler) getIterableExtensionKindForNode(node *ast.Node) (IterableExtensionKind, bool) {

	typ := t.checker.GetTypeAtLocation(node)
	if typ == nil {
		return "", false
	}
	return t.getIterableExtensionKindForType(typ)
}
