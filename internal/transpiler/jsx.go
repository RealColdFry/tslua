// TSTL delegates JSX to TypeScript's transformJsx which converts JSX to React.createElement()
// calls before TSTL processes them. Since tslua doesn't use tsgo's transformer pipeline,
// we handle JSX AST nodes directly, producing equivalent Lua output.
package transpiler

import (
	"strings"
	"unicode/utf8"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/microsoft/typescript-go/shim/core"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/microsoft/typescript-go/shim/scanner"
	"github.com/realcoldfry/tslua/internal/lua"
)

// transformJsxElement handles <Tag attrs>children</Tag>
func (t *Transpiler) transformJsxElement(node *ast.Node) lua.Expression {
	elem := node.AsJsxElement()
	opening := elem.OpeningElement
	tagName := t.getJsxTagName(opening)
	props := t.transformJsxAttributes(opening.Attributes())
	children := t.transformJsxChildren(elem.Children)
	return t.buildCreateElementCall(node, tagName, props, children)
}

// transformJsxSelfClosingElement handles <Tag attrs />
func (t *Transpiler) transformJsxSelfClosingElement(node *ast.Node) lua.Expression {
	elem := node.AsJsxSelfClosingElement()
	tagName := t.getJsxTagName(node)
	props := t.transformJsxAttributes(elem.Attributes)
	return t.buildCreateElementCall(node, tagName, props, nil)
}

// transformJsxFragment handles <>children</>
func (t *Transpiler) transformJsxFragment(node *ast.Node) lua.Expression {
	frag := node.AsJsxFragment()
	fragmentExpr := t.getJsxFragmentFactory()
	children := t.transformJsxChildren(frag.Children)
	return t.buildCreateElementCall(node, fragmentExpr, lua.Ident("nil"), children)
}

// buildCreateElementCall constructs factory(tagName, props, ...children).
// Handles self-parameter detection:
//   - Dotted factory (React.createElement) → colon method call (passes receiver as self)
//   - Simple factory (createElement) → checks if function needs self via type info;
//     if yes, passes nil as explicit self argument
func (t *Transpiler) buildCreateElementCall(node *ast.Node, tagName, props lua.Expression, children []lua.Expression) lua.Expression {
	prefix, method := t.getJsxFactoryParts()
	args := []lua.Expression{tagName, props}
	args = append(args, children...)
	if method != "" {
		call := lua.MethodCall(prefix, method, args...)
		t.setNodePos(call, node)
		return call
	}
	// Simple name factory: check if the function expects self.
	// If it does (e.g. it's an alias for a method), pass nil as self.
	if t.jsxFactoryNeedsSelf(node) {
		args = append([]lua.Expression{lua.Ident("nil")}, args...)
	}
	call := lua.Call(prefix, args...)
	t.setNodePos(call, node)
	return call
}

// getJsxTagName returns a string literal for intrinsic elements or an expression for components.
func (t *Transpiler) getJsxTagName(node *ast.Node) lua.Expression {
	tagName := node.TagName()
	switch tagName.Kind {
	case ast.KindIdentifier:
		text := tagName.AsIdentifier().Text
		if scanner.IsIntrinsicJsxName(text) {
			return lua.Str(text)
		}
		return t.transformExpression(tagName)
	case ast.KindJsxNamespacedName:
		nn := tagName.AsJsxNamespacedName()
		return lua.Str(nn.Namespace.Text() + ":" + nn.Name().Text())
	default:
		// Property access like <a.b.c />
		return t.transformExpression(tagName)
	}
}

// transformJsxAttributes converts JSX attributes to a Lua table expression (or nil).
func (t *Transpiler) transformJsxAttributes(attrsNode *ast.Node) lua.Expression {
	attrs := attrsNode.AsJsxAttributes()
	if attrs.Properties == nil || len(attrs.Properties.Nodes) == 0 {
		return lua.Ident("nil")
	}

	// Check for spread attributes
	hasSpread := false
	for _, attr := range attrs.Properties.Nodes {
		if attr.Kind == ast.KindJsxSpreadAttribute {
			hasSpread = true
			break
		}
	}

	if hasSpread {
		return t.transformJsxAttributesWithSpread(attrs.Properties.Nodes)
	}

	fields := make([]*lua.TableFieldExpression, 0, len(attrs.Properties.Nodes))
	for _, attr := range attrs.Properties.Nodes {
		if attr.Kind == ast.KindJsxAttribute {
			fields = append(fields, t.transformJsxAttribute(attr.AsJsxAttribute()))
		}
	}
	return &lua.TableExpression{Fields: fields}
}

// transformJsxAttributesWithSpread handles mixed regular and spread attributes.
// Produces __TS__ObjectAssign({}, spread1, {regular}, spread2, ...)
func (t *Transpiler) transformJsxAttributesWithSpread(attrs []*ast.Node) lua.Expression {
	fn := t.requireLualib("__TS__ObjectAssign")
	args := []lua.Expression{lua.Table()}

	var currentFields []*lua.TableFieldExpression
	for _, attr := range attrs {
		switch attr.Kind {
		case ast.KindJsxSpreadAttribute:
			if len(currentFields) > 0 {
				args = append(args, &lua.TableExpression{Fields: currentFields})
				currentFields = nil
			}
			sa := attr.AsJsxSpreadAttribute()
			args = append(args, t.transformExpression(sa.Expression))
		case ast.KindJsxAttribute:
			currentFields = append(currentFields, t.transformJsxAttribute(attr.AsJsxAttribute()))
		}
	}
	if len(currentFields) > 0 {
		args = append(args, &lua.TableExpression{Fields: currentFields})
	}
	return lua.Call(lua.Ident(fn), args...)
}

// transformJsxAttribute converts a single JSX attribute to a table field.
func (t *Transpiler) transformJsxAttribute(attr *ast.JsxAttribute) *lua.TableFieldExpression {
	key := t.getJsxAttributeName(attr)
	value := t.transformJsxAttributeInitializer(attr.Initializer)
	return lua.KeyField(key, value)
}

// getJsxAttributeName returns the key expression for an attribute.
// Non-identifier names (e.g., "foo-bar") become ["foo-bar"] bracket notation.
func (t *Transpiler) getJsxAttributeName(attr *ast.JsxAttribute) lua.Expression {
	name := attr.Name()
	if name.Kind == ast.KindIdentifier {
		text := name.AsIdentifier().Text
		if scanner.IsIdentifierText(text, core.LanguageVariantStandard) {
			return lua.Str(text)
		}
		return lua.Str(text)
	}
	// JsxNamespacedName
	nn := name.AsJsxNamespacedName()
	return lua.Str(nn.Namespace.Text() + ":" + nn.Name().Text())
}

// transformJsxAttributeInitializer converts an attribute value.
// nil initializer means boolean true shorthand (<a disabled />).
func (t *Transpiler) transformJsxAttributeInitializer(node *ast.Node) lua.Expression {
	if node == nil {
		return lua.Bool(true)
	}
	switch node.Kind {
	case ast.KindStringLiteral:
		return lua.Str(decodeEntities(node.Text()))
	case ast.KindJsxExpression:
		expr := node.AsJsxExpression()
		if expr.Expression == nil {
			return lua.Bool(true)
		}
		return t.transformExpression(expr.Expression)
	case ast.KindJsxElement:
		return t.transformJsxElement(node)
	case ast.KindJsxSelfClosingElement:
		return t.transformJsxSelfClosingElement(node)
	case ast.KindJsxFragment:
		return t.transformJsxFragment(node)
	default:
		return t.transformExpression(node)
	}
}

// transformJsxChildren transforms JSX child nodes into a slice of Lua expressions.
// Whitespace-only text and empty expressions are filtered out.
// Spread expressions ({...expr}) are unpacked with unpack().
func (t *Transpiler) transformJsxChildren(children *ast.NodeList) []lua.Expression {
	if children == nil || len(children.Nodes) == 0 {
		return nil
	}
	var result []lua.Expression
	for _, child := range children.Nodes {
		if child.Kind == ast.KindJsxExpression {
			jsxExpr := child.AsJsxExpression()
			if jsxExpr.Expression == nil {
				continue
			}
			expr := t.transformExpression(jsxExpr.Expression)
			if jsxExpr.DotDotDotToken != nil {
				// Spread children: {... expr} → unpack(expr)
				expr = lua.Call(t.unpackIdent(), expr)
			}
			result = append(result, expr)
			continue
		}
		expr := t.transformJsxChild(child)
		if expr != nil {
			result = append(result, expr)
		}
	}
	return result
}

// transformJsxChild transforms a single JSX child node.
func (t *Transpiler) transformJsxChild(node *ast.Node) lua.Expression {
	switch node.Kind {
	case ast.KindJsxText:
		return t.transformJsxText(node.AsJsxText())
	case ast.KindJsxExpression:
		return t.transformJsxExpression(node.AsJsxExpression())
	case ast.KindJsxElement:
		return t.transformJsxElement(node)
	case ast.KindJsxSelfClosingElement:
		return t.transformJsxSelfClosingElement(node)
	case ast.KindJsxFragment:
		return t.transformJsxFragment(node)
	default:
		return nil
	}
}

// transformJsxText processes JSX text content with whitespace trimming and entity decoding.
func (t *Transpiler) transformJsxText(text *ast.JsxText) lua.Expression {
	fixed := fixupWhitespaceAndDecodeEntities(text.Text)
	if len(fixed) == 0 {
		return nil
	}
	return lua.Str(fixed)
}

// transformJsxExpression handles {expr} in JSX. Empty expressions (comments) return nil.
func (t *Transpiler) transformJsxExpression(expr *ast.JsxExpression) lua.Expression {
	if expr.Expression == nil {
		return nil
	}
	return t.transformExpression(expr.Expression)
}

// getJsxFactoryName resolves the JSX factory name string.
// Priority: @jsx pragma > jsxFactory compiler option > React.createElement
func (t *Transpiler) getJsxFactoryName() string {
	// Check @jsx pragma
	jsxPragma := ast.GetPragmaFromSourceFile(t.sourceFile, "jsx")
	if jsxPragma != nil {
		factoryName := ast.GetPragmaArgument(jsxPragma, "factory")
		if factoryName != "" {
			return factoryName
		}
	}

	// Check jsxFactory compiler option
	if t.compilerOptions != nil && t.compilerOptions.JsxFactory != "" {
		return t.compilerOptions.JsxFactory
	}

	// Default
	return "React.createElement"
}

// getJsxFactoryParts returns the prefix expression and method name for the JSX factory.
// For dotted names like "React.createElement", returns (React, "createElement") for method call.
// For simple names like "createElement", returns (createElement, "") for regular call.
func (t *Transpiler) getJsxFactoryParts() (lua.Expression, string) {
	name := t.getJsxFactoryName()
	parts := strings.Split(name, ".")
	if len(parts) == 1 {
		return lua.Ident(parts[0]), ""
	}
	// Build prefix from all parts except last, last is the method name
	var prefix lua.Expression = lua.Ident(parts[0])
	for _, part := range parts[1 : len(parts)-1] {
		prefix = lua.Index(prefix, lua.Str(part))
	}
	return prefix, parts[len(parts)-1]
}

// getJsxFragmentFactory resolves the JSX fragment factory expression.
// Priority: @jsxFrag pragma > jsxFragmentFactory compiler option > React.Fragment
func (t *Transpiler) getJsxFragmentFactory() lua.Expression {
	// Check @jsxFrag pragma
	jsxFragPragma := ast.GetPragmaFromSourceFile(t.sourceFile, "jsxfrag")
	if jsxFragPragma != nil {
		factoryName := ast.GetPragmaArgument(jsxFragPragma, "factory")
		if factoryName != "" {
			return parseDottedName(factoryName)
		}
	}

	// Check jsxFragmentFactory compiler option
	if t.compilerOptions != nil && t.compilerOptions.JsxFragmentFactory != "" {
		return parseDottedName(t.compilerOptions.JsxFragmentFactory)
	}

	// Default: React.Fragment
	return lua.Index(lua.Ident("React"), lua.Str("Fragment"))
}

// jsxFactoryNeedsSelf checks whether the JSX factory function expects a self parameter.
// For simple-name factories, looks up the identifier's type and checks its call signatures.
func (t *Transpiler) jsxFactoryNeedsSelf(_ *ast.Node) bool {
	if t.checker == nil {
		return false
	}
	name := t.getJsxFactoryName()
	if strings.Contains(name, ".") {
		return false // dotted names use method call, not this path
	}
	// Find the factory identifier in the JSX element's scope.
	// The factory name is a simple identifier, so look it up via GetSymbolAtLocation
	// on the tagName of the JSX element (which is in the right scope).
	// Instead, we can find it by checking the source file's locals.

	// Use the type-based approach: get the type of the factory function
	// through the call signatures of the variable's type.
	// Walk the source file statements to find the factory identifier declaration.
	for _, stmt := range t.sourceFile.Statements.Nodes {
		if stmt.Kind == ast.KindVariableStatement {
			for _, decl := range stmt.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
				vd := decl.AsVariableDeclaration()
				if vd.Name().Kind == ast.KindIdentifier && vd.Name().AsIdentifier().Text == name {
					// Found the factory variable. Check its type's call signatures.
					sym := t.checker.GetSymbolAtLocation(vd.Name())
					if sym != nil {
						typ := t.checker.GetTypeOfSymbol(sym)
						if typ != nil {
							sigs := getAllCallSignatures(t.checker, typ)
							for _, s := range sigs {
								d := checker.Signature_declaration(s)
								if d != nil {
									ct := t.computeDeclarationContextType(d)
									if ct == contextNonVoid || ct == contextMixed {
										return true
									}
								}
							}
						}
					}
					return false
				}
			}
		}
	}
	return false
}

// parseDottedName converts "a.b.c" into nested Index expressions: a["b"]["c"]
func parseDottedName(name string) lua.Expression {
	parts := strings.Split(name, ".")
	var expr lua.Expression = lua.Ident(parts[0])
	for _, part := range parts[1:] {
		expr = lua.Index(expr, lua.Str(part))
	}
	return expr
}

// validateJsxConfig checks that JSX compiler options are valid for TSTL.
// Only jsx: "react" is supported. Returns true if config is valid.
func (t *Transpiler) validateJsxConfig(node *ast.Node) bool {
	if t.compilerOptions == nil {
		return true
	}
	jsx := t.compilerOptions.Jsx
	if jsx != core.JsxEmitNone && jsx != core.JsxEmitReact {
		t.addError(node, dw.UnsupportedJsxEmit, `JSX is only supported with "react" jsx option.`)
		return false
	}
	return true
}

// =========================================================================
// JSX text whitespace and entity processing
// =========================================================================

// fixupWhitespaceAndDecodeEntities processes JSX text content.
// It trims whitespace per the JSX whitespace rules and decodes HTML entities.
func fixupWhitespaceAndDecodeEntities(text string) string {
	var acc strings.Builder
	initial := true
	firstNonWhitespace := 0
	lastNonWhitespaceEnd := -1

	for i := 0; i < len(text); i++ {
		c, size := utf8.DecodeRuneInString(text[i:])
		if isLineBreak(c) {
			if firstNonWhitespace != -1 && lastNonWhitespaceEnd != -1 {
				addLineOfJsxText(&acc, text[firstNonWhitespace:lastNonWhitespaceEnd+1], initial)
				initial = false
			}
			firstNonWhitespace = -1
		} else if !isWhiteSpaceSingleLine(c) {
			lastNonWhitespaceEnd = i + size - 1
			if firstNonWhitespace == -1 {
				firstNonWhitespace = i
			}
		}

		if size > 1 {
			i += (size - 1)
		}
	}

	if firstNonWhitespace != -1 {
		addLineOfJsxText(&acc, text[firstNonWhitespace:], initial)
	}
	return acc.String()
}

func addLineOfJsxText(b *strings.Builder, trimmedLine string, isInitial bool) {
	decoded := decodeEntities(trimmedLine)
	if !isInitial {
		b.WriteString(" ")
	}
	b.WriteString(decoded)
}

func isLineBreak(c rune) bool {
	return c == '\n' || c == '\r' || c == '\u2028' || c == '\u2029'
}

func isWhiteSpaceSingleLine(c rune) bool {
	return c == ' ' || c == '\t' || c == '\v' || c == '\f' ||
		c == '\u00A0' || c == '\uFEFF' ||
		(c >= 0x2000 && c <= 0x200B) || c == 0x3000 || c == 0x205F
}

// decodeEntities replaces HTML entities like &amp; &#123; &#xDEAD; with their characters.
func decodeEntities(text string) string {
	i := strings.IndexByte(text, '&')
	if i < 0 {
		return text
	}

	var result strings.Builder
	result.Grow(len(text))
	for {
		result.WriteString(text[:i])
		text = text[i:]

		semi := strings.IndexByte(text, ';')
		if semi < 0 {
			break
		}

		entity := text[1:semi]
		decoded, ok := decodeEntity(entity)
		if ok {
			result.WriteRune(decoded)
		} else {
			result.WriteString(text[:semi+1])
		}
		text = text[semi+1:]

		i = strings.IndexByte(text, '&')
		if i < 0 {
			break
		}
	}
	result.WriteString(text)
	return result.String()
}

func decodeEntity(entity string) (rune, bool) {
	if len(entity) == 0 {
		return 0, false
	}

	if entity[0] == '#' {
		entity = entity[1:]
		if len(entity) == 0 {
			return 0, false
		}

		base := 10
		if entity[0] == 'x' || entity[0] == 'X' {
			base = 16
			entity = entity[1:]
		}

		if len(entity) == 0 {
			return 0, false
		}

		for _, c := range entity {
			if base == 16 && !isHexDigit(c) {
				return 0, false
			}
			if base == 10 && (c < '0' || c > '9') {
				return 0, false
			}
		}

		var val int64
		for _, c := range entity {
			val *= int64(base)
			if c >= '0' && c <= '9' {
				val += int64(c - '0')
			} else if c >= 'a' && c <= 'f' {
				val += int64(c-'a') + 10
			} else if c >= 'A' && c <= 'F' {
				val += int64(c-'A') + 10
			}
		}
		return rune(val), true
	}

	r, ok := htmlEntities[entity]
	return r, ok
}

func isHexDigit(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

var htmlEntities = map[string]rune{
	"quot":     0x0022,
	"amp":      0x0026,
	"apos":     0x0027,
	"lt":       0x003C,
	"gt":       0x003E,
	"nbsp":     0x00A0,
	"iexcl":    0x00A1,
	"cent":     0x00A2,
	"pound":    0x00A3,
	"curren":   0x00A4,
	"yen":      0x00A5,
	"brvbar":   0x00A6,
	"sect":     0x00A7,
	"uml":      0x00A8,
	"copy":     0x00A9,
	"ordf":     0x00AA,
	"laquo":    0x00AB,
	"not":      0x00AC,
	"shy":      0x00AD,
	"reg":      0x00AE,
	"macr":     0x00AF,
	"deg":      0x00B0,
	"plusmn":   0x00B1,
	"sup2":     0x00B2,
	"sup3":     0x00B3,
	"acute":    0x00B4,
	"micro":    0x00B5,
	"para":     0x00B6,
	"middot":   0x00B7,
	"cedil":    0x00B8,
	"sup1":     0x00B9,
	"ordm":     0x00BA,
	"raquo":    0x00BB,
	"frac14":   0x00BC,
	"frac12":   0x00BD,
	"frac34":   0x00BE,
	"iquest":   0x00BF,
	"Agrave":   0x00C0,
	"Aacute":   0x00C1,
	"Acirc":    0x00C2,
	"Atilde":   0x00C3,
	"Auml":     0x00C4,
	"Aring":    0x00C5,
	"AElig":    0x00C6,
	"Ccedil":   0x00C7,
	"Egrave":   0x00C8,
	"Eacute":   0x00C9,
	"Ecirc":    0x00CA,
	"Euml":     0x00CB,
	"Igrave":   0x00CC,
	"Iacute":   0x00CD,
	"Icirc":    0x00CE,
	"Iuml":     0x00CF,
	"ETH":      0x00D0,
	"Ntilde":   0x00D1,
	"Ograve":   0x00D2,
	"Oacute":   0x00D3,
	"Ocirc":    0x00D4,
	"Otilde":   0x00D5,
	"Ouml":     0x00D6,
	"times":    0x00D7,
	"Oslash":   0x00D8,
	"Ugrave":   0x00D9,
	"Uacute":   0x00DA,
	"Ucirc":    0x00DB,
	"Uuml":     0x00DC,
	"Yacute":   0x00DD,
	"THORN":    0x00DE,
	"szlig":    0x00DF,
	"agrave":   0x00E0,
	"aacute":   0x00E1,
	"acirc":    0x00E2,
	"atilde":   0x00E3,
	"auml":     0x00E4,
	"aring":    0x00E5,
	"aelig":    0x00E6,
	"ccedil":   0x00E7,
	"egrave":   0x00E8,
	"eacute":   0x00E9,
	"ecirc":    0x00EA,
	"euml":     0x00EB,
	"igrave":   0x00EC,
	"iacute":   0x00ED,
	"icirc":    0x00EE,
	"iuml":     0x00EF,
	"eth":      0x00F0,
	"ntilde":   0x00F1,
	"ograve":   0x00F2,
	"oacute":   0x00F3,
	"ocirc":    0x00F4,
	"otilde":   0x00F5,
	"ouml":     0x00F6,
	"divide":   0x00F7,
	"oslash":   0x00F8,
	"ugrave":   0x00F9,
	"uacute":   0x00FA,
	"ucirc":    0x00FB,
	"uuml":     0x00FC,
	"yacute":   0x00FD,
	"thorn":    0x00FE,
	"yuml":     0x00FF,
	"OElig":    0x0152,
	"oelig":    0x0153,
	"Scaron":   0x0160,
	"scaron":   0x0161,
	"Yuml":     0x0178,
	"fnof":     0x0192,
	"circ":     0x02C6,
	"tilde":    0x02DC,
	"Alpha":    0x0391,
	"Beta":     0x0392,
	"Gamma":    0x0393,
	"Delta":    0x0394,
	"Epsilon":  0x0395,
	"Zeta":     0x0396,
	"Eta":      0x0397,
	"Theta":    0x0398,
	"Iota":     0x0399,
	"Kappa":    0x039A,
	"Lambda":   0x039B,
	"Mu":       0x039C,
	"Nu":       0x039D,
	"Xi":       0x039E,
	"Omicron":  0x039F,
	"Pi":       0x03A0,
	"Rho":      0x03A1,
	"Sigma":    0x03A3,
	"Tau":      0x03A4,
	"Upsilon":  0x03A5,
	"Phi":      0x03A6,
	"Chi":      0x03A7,
	"Psi":      0x03A8,
	"Omega":    0x03A9,
	"alpha":    0x03B1,
	"beta":     0x03B2,
	"gamma":    0x03B3,
	"delta":    0x03B4,
	"epsilon":  0x03B5,
	"zeta":     0x03B6,
	"eta":      0x03B7,
	"theta":    0x03B8,
	"iota":     0x03B9,
	"kappa":    0x03BA,
	"lambda":   0x03BB,
	"mu":       0x03BC,
	"nu":       0x03BD,
	"xi":       0x03BE,
	"omicron":  0x03BF,
	"pi":       0x03C0,
	"rho":      0x03C1,
	"sigmaf":   0x03C2,
	"sigma":    0x03C3,
	"tau":      0x03C4,
	"upsilon":  0x03C5,
	"phi":      0x03C6,
	"chi":      0x03C7,
	"psi":      0x03C8,
	"omega":    0x03C9,
	"thetasym": 0x03D1,
	"upsih":    0x03D2,
	"piv":      0x03D6,
	"ensp":     0x2002,
	"emsp":     0x2003,
	"thinsp":   0x2009,
	"zwnj":     0x200C,
	"zwj":      0x200D,
	"lrm":      0x200E,
	"rlm":      0x200F,
	"ndash":    0x2013,
	"mdash":    0x2014,
	"lsquo":    0x2018,
	"rsquo":    0x2019,
	"sbquo":    0x201A,
	"ldquo":    0x201C,
	"rdquo":    0x201D,
	"bdquo":    0x201E,
	"dagger":   0x2020,
	"Dagger":   0x2021,
	"bull":     0x2022,
	"hellip":   0x2026,
	"permil":   0x2030,
	"prime":    0x2032,
	"Prime":    0x2033,
	"lsaquo":   0x2039,
	"rsaquo":   0x203A,
	"oline":    0x203E,
	"frasl":    0x2044,
	"euro":     0x20AC,
	"image":    0x2111,
	"weierp":   0x2118,
	"real":     0x211C,
	"trade":    0x2122,
	"alefsym":  0x2135,
	"larr":     0x2190,
	"uarr":     0x2191,
	"rarr":     0x2192,
	"darr":     0x2193,
	"harr":     0x2194,
	"crarr":    0x21B5,
	"lArr":     0x21D0,
	"uArr":     0x21D1,
	"rArr":     0x21D2,
	"dArr":     0x21D3,
	"hArr":     0x21D4,
	"nabla":    0x2207,
	"isin":     0x2208,
	"notin":    0x2209,
	"ni":       0x220B,
	"prod":     0x220F,
	"sum":      0x2211,
	"minus":    0x2212,
	"lowast":   0x2217,
	"radic":    0x221A,
	"prop":     0x221D,
	"infin":    0x221E,
	"ang":      0x2220,
	"and":      0x2227,
	"or":       0x2228,
	"cap":      0x2229,
	"cup":      0x222A,
	"int":      0x222B,
	"there4":   0x2234,
	"sim":      0x223C,
	"cong":     0x2245,
	"asymp":    0x2248,
	"ne":       0x2260,
	"equiv":    0x2261,
	"le":       0x2264,
	"ge":       0x2265,
	"sub":      0x2282,
	"sup":      0x2283,
	"nsub":     0x2284,
	"sube":     0x2286,
	"supe":     0x2287,
	"oplus":    0x2295,
	"otimes":   0x2297,
	"perp":     0x22A5,
	"sdot":     0x22C5,
	"lceil":    0x2308,
	"rceil":    0x2309,
	"lfloor":   0x230A,
	"rfloor":   0x230B,
	"lang":     0x2329,
	"rang":     0x232A,
	"loz":      0x25CA,
	"spades":   0x2660,
	"clubs":    0x2663,
	"hearts":   0x2665,
	"diams":    0x2666,
}
