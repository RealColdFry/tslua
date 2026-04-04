package transpiler

import (
	"fmt"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

func (t *Transpiler) transformArrayLiteral(node *ast.Node) lua.Expression {
	al := node.AsArrayLiteralExpression()
	if al.Elements == nil || len(al.Elements.Nodes) == 0 {
		return lua.Table()
	}

	elements := al.Elements.Nodes
	t.checkUndefinedInArrayLiteral(elements)

	// Use transformExpressionList for all cases to preserve execution order
	// when any element has side effects (e.g. i++ in [i, i++, i])
	exprs := t.transformExpressionList(elements)
	var fields []*lua.TableFieldExpression
	for _, expr := range exprs {
		fields = append(fields, lua.Field(expr))
	}
	return &lua.TableExpression{Fields: fields}
}

// checkUndefinedInArrayLiteral reports an error for non-trailing undefined/null in array literals.
func (t *Transpiler) checkUndefinedInArrayLiteral(elements []*ast.Node) {
	// Find last non-nil element
	lastNonNil := len(elements) - 1
	for ; lastNonNil >= 0; lastNonNil-- {
		if !isUndefinedOrNull(elements[lastNonNil]) {
			break
		}
	}
	// Report non-trailing undefined/null
	for i := 0; i < lastNonNil; i++ {
		if isUndefinedOrNull(elements[i]) {
			t.addError(elements[i], dw.UndefinedInArrayLiteral, "Array literals may not contain undefined or null.")
		}
	}
}

func isUndefinedOrNull(node *ast.Node) bool {
	if node.Kind == ast.KindNullKeyword {
		return true
	}
	if node.Kind == ast.KindIdentifier && node.AsIdentifier().Text == "undefined" {
		return true
	}
	return false
}

func (t *Transpiler) transformObjectLiteral(node *ast.Node) lua.Expression {
	ol := node.AsObjectLiteralExpression()
	if ol.Properties == nil || len(ol.Properties.Nodes) == 0 {
		return lua.Table()
	}

	// SpreadAssignment → __TS__ObjectAssign({}, spread1, {props}, spread2, ...)
	for _, prop := range ol.Properties.Nodes {
		if prop.Kind == ast.KindSpreadAssignment {
			return t.transformObjectLiteralWithSpread(ol)
		}
	}

	// Transform each property's key and value in separate preceding statement scopes
	// to detect side effects. If a later property has side effects, earlier keys and
	// values must be cached in temps to preserve evaluation order.
	n := len(ol.Properties.Nodes)
	fields := make([]*lua.TableFieldExpression, n)
	keyPrecs := make([][]lua.Statement, n)
	valPrecs := make([][]lua.Statement, n)
	isComputed := make([]bool, n)
	lastPrecIdx := -1
	for i, prop := range ol.Properties.Nodes {
		t.pushPrecedingStatements()
		key := t.transformObjectPropertyKey(prop)
		keyPrecs[i] = t.popPrecedingStatements()

		t.pushPrecedingStatements()
		val := t.transformObjectPropertyValue(prop)
		valPrecs[i] = t.popPrecedingStatements()

		if prop.Kind == ast.KindPropertyAssignment && prop.AsPropertyAssignment().Name().Kind == ast.KindComputedPropertyName {
			isComputed[i] = true
			fields[i] = lua.ComputedKeyField(key, val)
		} else {
			fields[i] = lua.KeyField(key, val)
		}
		if len(keyPrecs[i]) > 0 || len(valPrecs[i]) > 0 {
			lastPrecIdx = i
		}
	}

	if lastPrecIdx < 0 {
		return &lua.TableExpression{Fields: fields}
	}

	// Some properties have preceding statements — cache earlier keys and values
	for i := 0; i < n; i++ {
		t.addPrecedingStatements(keyPrecs[i]...)
		// Only cache computed keys — static keys (identifiers, strings) are constant
		if isComputed[i] && i <= lastPrecIdx && fields[i].Key != nil && t.shouldMoveToTemp(fields[i].Key) {
			temp := t.nextTemp("arg")
			t.addPrecedingStatements(lua.LocalDecl([]*lua.Identifier{lua.Ident(temp)}, []lua.Expression{fields[i].Key}))
			fields[i].Key = lua.Ident(temp)
		}
		t.addPrecedingStatements(valPrecs[i]...)
		if i < lastPrecIdx && fields[i].Value != nil && t.shouldMoveToTemp(fields[i].Value) {
			fields[i].Value = t.moveToPrecedingTemp(fields[i].Value)
		}
	}
	return &lua.TableExpression{Fields: fields}
}

func (t *Transpiler) transformObjectPropertyKey(prop *ast.Node) lua.Expression {
	switch prop.Kind {
	case ast.KindPropertyAssignment:
		return t.objectPropertyKeyExpr(prop.AsPropertyAssignment().Name())
	case ast.KindShorthandPropertyAssignment:
		// TSTL always emits shorthand property keys as strings (transformPropertyName).
		return lua.Str(prop.AsShorthandPropertyAssignment().Name().AsIdentifier().Text)
	case ast.KindMethodDeclaration:
		return t.objectPropertyKeyExpr(prop.AsMethodDeclaration().Name())
	default:
		return nil
	}
}

func (t *Transpiler) transformObjectPropertyValue(prop *ast.Node) lua.Expression {
	switch prop.Kind {
	case ast.KindPropertyAssignment:
		return t.transformExpression(prop.AsPropertyAssignment().Initializer)
	case ast.KindShorthandPropertyAssignment:
		spa := prop.AsShorthandPropertyAssignment()
		name := spa.Name().AsIdentifier().Text
		var valueSym *ast.Symbol
		if t.inScope() && t.checker != nil {
			valueSym = checker.Checker_GetShorthandAssignmentValueSymbol(t.checker, prop)
			if valueSym != nil {
				t.trackSymbolReference(valueSym, spa.Name())
			}
		}
		var valExpr lua.Expression
		if valueSym != nil && t.shouldRenameValueSymbol(name, valueSym) {
			valExpr = lua.Ident(luaSafeName(name))
		} else if !isValidLuaIdentifier(name, t.luaTarget.AllowsUnicodeIds()) {
			valExpr = lua.Ident(luaSafeName(name))
		} else {
			valExpr = t.transformExpression(spa.Name())
		}
		if !isValidLuaIdentifier(name, t.luaTarget.AllowsUnicodeIds()) {
			symbol := t.checker.GetSymbolAtLocation(spa.Name())
			isValueDeclared := false
			if symbol != nil {
				for _, decl := range symbol.Declarations {
					if decl.Kind != ast.KindShorthandPropertyAssignment {
						isValueDeclared = true
						break
					}
				}
			}
			if !isValueDeclared {
				t.addError(spa.Name(), dw.InvalidAmbientIdentifierName, fmt.Sprintf(
					"Invalid ambient identifier name '%s'. Ambient identifiers must be valid lua identifiers.", name))
			}
		}
		return valExpr
	case ast.KindMethodDeclaration:
		md := prop.AsMethodDeclaration()
		needsSelf := t.functionNeedsSelf(prop)
		paramIdents, hasRest := t.transformParamIdents(md.Parameters, needsSelf)
		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, t.transformParamPreamble(md.Parameters)...)
		if md.Body != nil {
			bodyStmts = append(bodyStmts, t.transformBlock(md.Body)...)
		}
		var result lua.Expression = &lua.FunctionExpression{
			Params: paramIdents,
			Dots:   hasRest,
			Body:   &lua.Block{Statements: bodyStmts},
		}
		// Wrap generator methods in __TS__Generator
		if md.AsteriskToken != nil {
			genFn := t.requireLualib("__TS__Generator")
			result = lua.Call(lua.Ident(genFn), result)
		}
		return result
	case ast.KindGetAccessor, ast.KindSetAccessor:
		t.addError(prop, dw.UnsupportedAccessorInObjectLiteral, "Accessors in object literal are not supported.")
		return lua.Nil()
	default:
		return lua.Comment(fmt.Sprintf("TODO: obj prop kind %d", prop.Kind))
	}
}

func (t *Transpiler) transformObjectPropertyField(prop *ast.Node) *lua.TableFieldExpression {
	switch prop.Kind {
	case ast.KindPropertyAssignment:
		pa := prop.AsPropertyAssignment()
		key := t.objectPropertyKeyExpr(pa.Name())
		val := t.transformExpression(pa.Initializer)
		if pa.Name().Kind == ast.KindComputedPropertyName {
			return lua.ComputedKeyField(key, val)
		}
		return lua.KeyField(key, val)
	case ast.KindShorthandPropertyAssignment:
		spa := prop.AsShorthandPropertyAssignment()
		name := spa.Name().AsIdentifier().Text
		// Track the VALUE symbol for hoisting analysis.
		// GetSymbolAtLocation returns the property symbol, not the value variable.
		var valueSym *ast.Symbol
		if t.inScope() && t.checker != nil {
			valueSym = checker.Checker_GetShorthandAssignmentValueSymbol(t.checker, prop)
			if valueSym != nil {
				t.trackSymbolReference(valueSym, spa.Name())
			}
		}
		// For shorthand { x }, the name is both the property key and value reference.
		// transformExpression(spa.Name()) would use the property symbol (which is never renamed).
		// We need to check the VALUE symbol to determine if the identifier was renamed.
		var valExpr lua.Expression
		if valueSym != nil && t.shouldRenameValueSymbol(name, valueSym) {
			valExpr = lua.Ident(luaSafeName(name))
		} else if !isValidLuaIdentifier(name, t.luaTarget.AllowsUnicodeIds()) {
			// Ambient/undeclared identifiers with invalid Lua names must still be renamed.
			// TSTL handles this via transformIdentifierWithSymbol which always applies luaSafeName.
			valExpr = lua.Ident(luaSafeName(name))
		} else {
			valExpr = t.transformExpression(spa.Name())
		}
		// Check if the value reference is an undeclared ambient with invalid name — emit diagnostic.
		if !isValidLuaIdentifier(name, t.luaTarget.AllowsUnicodeIds()) {
			// Try to get the value symbol — for undeclared vars, GetSymbolAtLocation
			// returns the property symbol. Check if there's a real value declaration.
			symbol := t.checker.GetSymbolAtLocation(spa.Name())
			isValueDeclared := false
			if symbol != nil {
				for _, decl := range symbol.Declarations {
					if decl.Kind != ast.KindShorthandPropertyAssignment {
						isValueDeclared = true
						break
					}
				}
			}
			if !isValueDeclared {
				t.addError(spa.Name(), dw.InvalidAmbientIdentifierName, fmt.Sprintf(
					"Invalid ambient identifier name '%s'. Ambient identifiers must be valid lua identifiers.", name))
			}
		}
		return lua.KeyField(lua.Str(name), valExpr)
	case ast.KindMethodDeclaration:
		md := prop.AsMethodDeclaration()
		key := t.objectPropertyKeyExpr(md.Name())
		paramIdents, hasRest := t.transformParamIdents(md.Parameters, true)
		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, t.transformParamPreamble(md.Parameters)...)
		if md.Body != nil {
			bodyStmts = append(bodyStmts, t.transformBlock(md.Body)...)
		}
		fn := &lua.FunctionExpression{
			Params: paramIdents,
			Dots:   hasRest,
			Body:   &lua.Block{Statements: bodyStmts},
		}
		return lua.KeyField(key, fn)
	case ast.KindGetAccessor, ast.KindSetAccessor:
		t.addError(prop, dw.UnsupportedAccessorInObjectLiteral, "Accessors in object literal are not supported.")
		return lua.Field(lua.Nil())
	case ast.KindSpreadAssignment:
		return lua.Field(lua.Comment("spread handled elsewhere"))
	default:
		return lua.Field(lua.Comment(fmt.Sprintf("TODO: obj prop kind %d", prop.Kind)))
	}
}

func (t *Transpiler) objectPropertyKeyExpr(name *ast.Node) lua.Expression {
	switch name.Kind {
	case ast.KindIdentifier:
		text := name.AsIdentifier().Text
		if !isValidLuaIdentifier(text, false) {
			return lua.Str(text)
		}
		return lua.Ident(text)
	case ast.KindStringLiteral:
		text := name.AsStringLiteral().Text
		if isValidLuaIdentifier(text, false) {
			return lua.Ident(text)
		}
		return lua.Str(text)
	case ast.KindNumericLiteral:
		return lua.Num(name.AsNumericLiteral().Text)
	case ast.KindComputedPropertyName:
		return t.transformExpression(name.AsComputedPropertyName().Expression)
	default:
		return t.transformExpression(name)
	}
}

// __TS__ObjectAssign({}, spread1, {key = val}, spread2, {key2 = val2})
func (t *Transpiler) transformObjectLiteralWithSpread(ol *ast.ObjectLiteralExpression) lua.Expression {
	fn := t.requireLualib("__TS__ObjectAssign")
	var args []lua.Expression
	args = append(args, lua.Table())

	var currentFields []*lua.TableFieldExpression
	for _, prop := range ol.Properties.Nodes {
		if prop.Kind == ast.KindSpreadAssignment {
			if len(currentFields) > 0 {
				args = append(args, &lua.TableExpression{Fields: currentFields})
				currentFields = nil
			}
			sa := prop.AsSpreadAssignment()
			spreadExpr := t.transformExpression(sa.Expression)
			// Arrays need to be converted to objects when spread into object literals
			if t.isArrayType(sa.Expression) {
				arrToObj := t.requireLualib("__TS__ArrayToObject")
				spreadExpr = lua.Call(lua.Ident(arrToObj), spreadExpr)
			}
			args = append(args, spreadExpr)
		} else {
			currentFields = append(currentFields, t.transformObjectPropertyField(prop))
		}
	}
	if len(currentFields) > 0 {
		args = append(args, &lua.TableExpression{Fields: currentFields})
	}

	return lua.Call(lua.Ident(fn), args...)
}

// `hello ${count} world` → "hello " .. tostring(count) .. " world"
func (t *Transpiler) transformTemplateExpression(node *ast.Node) lua.Expression {
	te := node.AsTemplateExpression()

	// Collect span expressions and transform in scopes to detect side effects
	spans := te.TemplateSpans.Nodes
	n := len(spans)
	exprs := make([]lua.Expression, n)
	precs := make([][]lua.Statement, n)
	lastPrecIdx := -1
	for i, span := range spans {
		ts := span.AsTemplateSpan()
		exprs[i], precs[i] = t.transformExprInScope(ts.Expression)
		if !t.isNonNullStringExpression(ts.Expression) {
			exprs[i] = lua.Call(lua.Ident("tostring"), exprs[i])
		}
		if len(precs[i]) > 0 {
			lastPrecIdx = i
		}
	}

	// If any span has side effects, cache earlier spans in temps
	if lastPrecIdx > 0 {
		for i := 0; i < n; i++ {
			t.addPrecedingStatements(precs[i]...)
			if i < lastPrecIdx {
				exprs[i] = t.moveToPrecedingTemp(exprs[i])
			}
		}
	} else if lastPrecIdx == 0 {
		t.addPrecedingStatements(precs[0]...)
	}

	// Build parts array, then reduce with concat (matches TSTL's approach).
	// Only add head if non-empty to avoid leading `"" ..`.
	var parts []lua.Expression
	headText := te.Head.AsTemplateHead().Text
	if headText != "" {
		parts = append(parts, lua.Str(headText))
	}

	for i, span := range spans {
		ts := span.AsTemplateSpan()
		parts = append(parts, exprs[i])

		var literalText string
		if ts.Literal.Kind == ast.KindTemplateMiddle {
			literalText = ts.Literal.AsTemplateMiddle().Text
		} else {
			literalText = ts.Literal.AsTemplateTail().Text
		}

		if literalText != "" {
			parts = append(parts, lua.Str(literalText))
		}
	}

	if len(parts) == 0 {
		return lua.Str("")
	}

	result := parts[0]
	for _, part := range parts[1:] {
		result = lua.Binary(result, lua.OpConcat, part)
	}
	return result
}

// getTemplateRawLiteral extracts the raw source text from a template literal node,
// preserving escape sequences as written. Mirrors TSTL's getRawLiteral.
func (t *Transpiler) getTemplateRawLiteral(node *ast.Node) string {
	if data := node.TemplateLiteralLikeData(); data != nil && data.RawText != "" {
		return data.RawText
	}
	sourceText := t.sourceFile.Text()
	end := node.End()
	if end <= 0 || int(end) > len(sourceText) {
		return node.Text()
	}
	start := node.Pos()
	for i := start; i < end; i++ {
		if sourceText[i] == '`' || sourceText[i] == '}' {
			start = i
			break
		}
	}
	text := sourceText[start:end]
	isLast := node.Kind == ast.KindNoSubstitutionTemplateLiteral || node.Kind == ast.KindTemplateTail
	endTrim := 2
	if isLast {
		endTrim = 1
	}
	if len(text) < 1+endTrim {
		return node.Text()
	}
	text = text[1 : len(text)-endTrim]
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	// Convert JS unicode escapes (\uXXXX) to Lua decimal escapes (\XXX)
	text = convertUnicodeEscapes(text)
	return text
}

// convertUnicodeEscapes converts \uXXXX patterns in raw template text to Lua \XXX decimal escapes.
func convertUnicodeEscapes(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+5 < len(s) && s[i] == '\\' && s[i+1] == 'u' {
			// Try to parse 4 hex digits
			hex := s[i+2 : i+6]
			var code int
			if _, err := fmt.Sscanf(hex, "%04x", &code); err == nil {
				fmt.Fprintf(&result, "\\%d", code)
				i += 6
				continue
			}
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// func`hello ${expr}` → func({[1]="hello ", [2]="", raw={"hello ", ""}}, expr)
func (t *Transpiler) transformTaggedTemplateExpression(node *ast.Node) lua.Expression {
	tte := node.AsTaggedTemplateExpression()

	var strings []string
	var rawStrings []string
	var exprNodes []*ast.Node

	template := tte.Template
	if template.Kind == ast.KindTemplateExpression {
		te := template.AsTemplateExpression()
		head := te.Head.AsTemplateHead()
		strings = append(strings, head.Text)
		rawStrings = append(rawStrings, t.getTemplateRawLiteral(te.Head))
		for _, span := range te.TemplateSpans.Nodes {
			ts := span.AsTemplateSpan()
			strings = append(strings, ts.Literal.Text())
			rawStrings = append(rawStrings, t.getTemplateRawLiteral(ts.Literal))
			exprNodes = append(exprNodes, ts.Expression)
		}
	} else {
		// NoSubstitutionTemplateLiteral
		nst := template.AsNoSubstitutionTemplateLiteral()
		strings = append(strings, nst.Text)
		rawStrings = append(rawStrings, t.getTemplateRawLiteral(template))
	}

	// Build the strings table: {[1]="text1", [2]="text2", raw={"raw1", "raw2"}}
	var tableFields []*lua.TableFieldExpression
	for i, s := range strings {
		tableFields = append(tableFields, lua.KeyField(lua.Num(fmt.Sprintf("%d", i+1)), lua.Str(s)))
	}
	var rawFields []*lua.TableFieldExpression
	for _, r := range rawStrings {
		rawFields = append(rawFields, lua.Field(lua.Str(r)))
	}
	tableFields = append(tableFields, lua.KeyField(lua.Ident("raw"), lua.Table(rawFields...)))

	stringsTable := lua.Table(tableFields...)

	// Transform span expressions using transformExpressionList to preserve evaluation order
	exprArgs := t.transformExpressionList(exprNodes)

	// Build arguments
	tag := t.transformExpression(tte.Tag)
	var args []lua.Expression

	// Check if tag is a method call (obj.func or obj["func"])
	switch tte.Tag.Kind {
	case ast.KindPropertyAccessExpression:
		pa := tte.Tag.AsPropertyAccessExpression()
		obj := t.transformExpression(pa.Expression)
		method := pa.Name().AsIdentifier().Text
		if t.calleeNeedsSelfForTag(node) {
			args = append(args, obj, stringsTable)
			args = append(args, exprArgs...)
			return lua.Call(lua.Index(obj, lua.Str(method)), args...)
		}
	case ast.KindElementAccessExpression:
		ea := tte.Tag.AsElementAccessExpression()
		obj := t.transformExpression(ea.Expression)
		idx := t.transformExpression(ea.ArgumentExpression)
		if t.calleeNeedsSelfForTag(node) {
			args = append(args, obj, stringsTable)
			args = append(args, exprArgs...)
			return lua.Call(lua.Index(obj, idx), args...)
		}
	}

	args = append(args, stringsTable)
	args = append(args, exprArgs...)

	if t.calleeNeedsSelfForTag(node) {
		args = append([]lua.Expression{t.defaultSelfContext()}, args...)
	}

	return lua.Call(tag, args...)
}

// calleeNeedsSelfForTag reuses the same self-detection logic as calleeNeedsSelf.
// TaggedTemplateExpression also supports getResolvedSignature.
func (t *Transpiler) calleeNeedsSelfForTag(node *ast.Node) bool {
	return t.getCallContextType(node) != contextVoid
}
