package transpiler

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
	"github.com/realcoldfry/tslua/internal/lua"
)

func (t *Transpiler) transformClassExpression(node *ast.Node) lua.Expression {
	ce := node.AsClassExpression()
	name := ""
	origName := ""
	if ce.Name() != nil {
		origName = ce.Name().AsIdentifier().Text
		name = origName
		if t.hasUnsafeIdentifierName(ce.Name()) {
			name = luaSafeName(origName)
		}
	} else {
		name = t.nextTemp("class")
		// Derive display name from parent variable declaration or export default
		origName = ""
		if node.Parent != nil && node.Parent.Kind == ast.KindVariableDeclaration {
			vd := node.Parent.AsVariableDeclaration()
			if vd.Name().Kind == ast.KindIdentifier {
				origName = vd.Name().AsIdentifier().Text
			}
		} else if node.Parent != nil && node.Parent.Kind == ast.KindExportAssignment {
			origName = "default"
		} else if node.Parent != nil && node.Parent.Kind == ast.KindBinaryExpression {
			// Assignment: const x = class {} — check LHS
			be := node.Parent.AsBinaryExpression()
			if be.Left.Kind == ast.KindIdentifier {
				origName = be.Left.AsIdentifier().Text
			}
		}
	}

	var stmts []lua.Statement

	// Determine base class expression if any
	var baseExpr lua.Expression
	if ce.HeritageClauses != nil {
		for _, clause := range ce.HeritageClauses.Nodes {
			hc := clause.AsHeritageClause()
			if hc.Token == ast.KindExtendsKeyword && hc.Types != nil && len(hc.Types.Nodes) > 0 {
				baseExpr = t.transformExpression(hc.Types.Nodes[0].AsExpressionWithTypeArguments().Expression)
			}
		}
	}

	cs := t.classStyle
	if cs.isInline() {
		// Inline style: wrap in do block with setmetatable boilerplate
		stmts = append(stmts, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(name)},
			nil,
		))
		var doBody []lua.Statement
		doBody = append(doBody, cs.inlineInitStatements(lua.Ident(name), origName, baseExpr)...)

		prevBaseClassName := t.currentBaseClassName
		prevClassRef := t.currentClassRef
		t.currentClassRef = lua.Ident(name)
		t.currentBaseClassName = t.getBaseClassName(ce.HeritageClauses)

		doBody = append(doBody, t.transformClassMembersShared(node, lua.Ident(name))...)

		t.currentBaseClassName = prevBaseClassName
		t.currentClassRef = prevClassRef

		if ast.HasDecorators(node) {
			decoratingExpr := t.createClassDecoratingExpression(node, lua.Ident(name), origName)
			doBody = append(doBody, lua.Assign(
				[]lua.Expression{lua.Ident(name)},
				[]lua.Expression{decoratingExpr},
			))
		}

		stmts = append(stmts, lua.Do(doBody...))
	} else if initExpr := cs.classInitExpr(origName, baseExpr); initExpr != nil {
		// Alternative class style: class("Name")(base) or class("Name", base)
		stmts = append(stmts, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(name)},
			[]lua.Expression{initExpr},
		))
		stmts = append(stmts, lua.Assign(
			[]lua.Expression{lua.Index(lua.Ident(name), lua.Str("__name"))},
			[]lua.Expression{lua.Str(origName)},
		))

		prevBaseClassName := t.currentBaseClassName
		prevClassRef := t.currentClassRef
		t.currentClassRef = lua.Ident(name)
		t.currentBaseClassName = t.getBaseClassName(ce.HeritageClauses)

		stmts = append(stmts, t.transformClassMembersShared(node, lua.Ident(name))...)

		t.currentBaseClassName = prevBaseClassName
		t.currentClassRef = prevClassRef

		if ast.HasDecorators(node) {
			decoratingExpr := t.createClassDecoratingExpression(node, lua.Ident(name), origName)
			stmts = append(stmts, lua.Assign(
				[]lua.Expression{lua.Ident(name)},
				[]lua.Expression{decoratingExpr},
			))
		}
	} else {
		// TSTL default: __TS__Class() + name + __TS__ClassExtends
		stmts = append(stmts, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(name)},
			[]lua.Expression{lua.Call(lua.Ident(t.requireLualib("__TS__Class")))},
		))
		// Name assignment: use self-reference when no name derivation is available
		// and the class has a base (preserves name set by __TS__ClassExtends runtime)
		hasHeritage := ce.HeritageClauses != nil && len(ce.HeritageClauses.Nodes) > 0
		if origName == "" && hasHeritage {
			stmts = append(stmts, lua.Assign(
				[]lua.Expression{lua.Index(lua.Ident(name), lua.Str("name"))},
				[]lua.Expression{lua.Index(lua.Ident(name), lua.Str("name"))},
			))
		} else {
			stmts = append(stmts, lua.Assign(
				[]lua.Expression{lua.Index(lua.Ident(name), lua.Str("name"))},
				[]lua.Expression{lua.Str(origName)},
			))
		}

		// Inheritance (TSTL path)
		if baseExpr != nil {
			classExtends := t.requireLualib("__TS__ClassExtends")
			stmts = append(stmts, lua.ExprStmt(lua.Call(lua.Ident(classExtends), lua.Ident(name), baseExpr)))
		}

		prevBaseClassName := t.currentBaseClassName
		prevClassRef := t.currentClassRef
		t.currentClassRef = lua.Ident(name)
		t.currentBaseClassName = t.getBaseClassName(ce.HeritageClauses)

		stmts = append(stmts, t.transformClassMembersShared(node, lua.Ident(name))...)

		t.currentBaseClassName = prevBaseClassName
		t.currentClassRef = prevClassRef

		// Class decorators on class expressions
		if ast.HasDecorators(node) {
			decoratingExpr := t.createClassDecoratingExpression(node, lua.Ident(name), origName)
			stmts = append(stmts, lua.Assign(
				[]lua.Expression{lua.Ident(name)},
				[]lua.Expression{decoratingExpr},
			))
		}
	}

	t.addPrecedingStatements(stmts...)
	return lua.Ident(name)
}

func (t *Transpiler) transformClassDeclaration(node *ast.Node) []lua.Statement {
	cd := node.AsClassDeclaration()
	origName := ""
	name := ""
	if cd.Name() != nil {
		origName = cd.Name().AsIdentifier().Text
		name = origName
		// Check @customName annotation
		if customName := t.getCustomName(node); customName != "" {
			name = customName
			origName = customName
		}
		// Class names used as local variables must be valid Lua identifiers
		if !isValidLuaIdentifier(name, t.luaTarget.AllowsUnicodeIds()) || isLuaBuiltin(name) {
			name = luaSafeName(origName)
		}
	} else {
		name = t.nextTemp("class")
		// Only use "default" name for export default class declarations.
		// Bare anonymous class declarations (class {}) get empty name.
		if hasDefaultModifier(node) {
			origName = "default"
		}
	}
	isExported := hasExportModifier(node)
	isDefault := hasDefaultModifier(node)

	comments := t.getLeadingComments(node)

	// Determine base class expression if any
	var baseExpr lua.Expression
	var basePrec []lua.Statement
	if cd.HeritageClauses != nil {
		for _, clause := range cd.HeritageClauses.Nodes {
			hc := clause.AsHeritageClause()
			if hc.Token == ast.KindExtendsKeyword && hc.Types != nil && len(hc.Types.Nodes) > 0 {
				baseExpr, basePrec = t.transformExprInScope(hc.Types.Nodes[0].AsExpressionWithTypeArguments().Expression)
			}
		}
	}

	var result []lua.Statement
	if len(basePrec) > 0 {
		result = append(result, basePrec...)
	}

	cs := t.classStyle

	exportTarget := "____exports"
	if t.currentNamespace != "" {
		exportTarget = t.currentNamespace
	}
	exportKey := origName
	if isDefault {
		exportKey = "default"
	}

	if cs.isInline() {
		// Inline style: emit local declaration, then do block with setmetatable boilerplate
		if isExported && t.isExportAsGlobalTopLevel() {
			// global scope — no local, assign handled inside do block
		} else if isExported {
			// exported — assign to exports, then local alias
		} else if t.shouldUseLocalDeclaration() {
			decl := lua.LocalDecl([]*lua.Identifier{lua.Ident(name)}, nil)
			if t.inScope() {
				var symID SymbolID
				if cd.Name() != nil {
					if sym := t.checker.GetSymbolAtLocation(cd.Name()); sym != nil {
						symID = t.getOrCreateSymbolID(sym)
					}
				}
				t.addScopeVariableDeclaration(decl, symID)
			}
			result = append(result, decl)
		}

		var classRef lua.Expression = lua.Ident(name)
		var doBody []lua.Statement
		doBody = append(doBody, cs.inlineInitStatements(classRef, origName, baseExpr)...)

		if isExported && !t.isExportAsGlobalTopLevel() {
			// Assign to exports after setmetatable init
			doBody = append(doBody, lua.Assign(
				[]lua.Expression{lua.Index(lua.Ident(exportTarget), lua.Str(exportKey))},
				[]lua.Expression{classRef},
			))
		}

		prevBaseClassName := t.currentBaseClassName
		prevClassRef := t.currentClassRef
		t.currentClassRef = classRef
		t.currentBaseClassName = t.getBaseClassName(cd.HeritageClauses)

		doBody = append(doBody, t.transformClassMembersShared(node, classRef)...)

		t.currentBaseClassName = prevBaseClassName
		t.currentClassRef = prevClassRef

		if ast.HasDecorators(node) {
			decoratingExpr := t.createClassDecoratingExpression(node, classRef, origName)
			doBody = append(doBody, lua.Assign(
				[]lua.Expression{classRef},
				[]lua.Expression{decoratingExpr},
			))
			if isExported && !t.isExportAsGlobalTopLevel() {
				doBody = append(doBody, lua.Assign(
					[]lua.Expression{lua.Index(lua.Ident(exportTarget), lua.Str(exportKey))},
					[]lua.Expression{classRef},
				))
			}
		}

		result = append(result, lua.Do(doBody...))
	} else {
		// Library-based styles (TSTL, luabind, middleclass)
		var classInitExpr lua.Expression
		if initExpr := cs.classInitExpr(origName, baseExpr); initExpr != nil {
			classInitExpr = initExpr
		} else {
			classNew := t.requireLualib("__TS__Class")
			classInitExpr = lua.Call(lua.Ident(classNew))
		}

		// Create class name identifier. Only set position when the name was
		// renamed (e.g. $$$ → ____24__24__24_) so the source map carries the
		// original name. Otherwise, the statement-level mapping (set from the
		// ClassDeclaration node) provides the correct position at the "class"
		// keyword — setting the identifier position would overwrite it with the
		// name position (column 6 in "class Bar").
		classNameIdent := lua.Ident(name)
		if cd.Name() != nil && name != origName {
			t.setNodePosNamed(classNameIdent, cd.Name(), origName)
		}

		if isExported && t.isExportAsGlobalTopLevel() {
			initStmt := lua.Assign(
				[]lua.Expression{classNameIdent},
				[]lua.Expression{classInitExpr},
			)
			t.setNodePos(initStmt, node)
			result = append(result, initStmt)
		} else if isExported {
			initStmt := lua.Assign(
				[]lua.Expression{lua.Index(lua.Ident(exportTarget), lua.Str(exportKey))},
				[]lua.Expression{classInitExpr},
			)
			t.setNodePos(initStmt, node)
			result = append(result, initStmt)
		} else if t.shouldUseLocalDeclaration() {
			decl := lua.LocalDecl(
				[]*lua.Identifier{classNameIdent},
				[]lua.Expression{classInitExpr},
			)
			t.setNodePos(decl, node)
			if t.inScope() {
				var symID SymbolID
				if cd.Name() != nil {
					if sym := t.checker.GetSymbolAtLocation(cd.Name()); sym != nil {
						symID = t.getOrCreateSymbolID(sym)
					}
				}
				t.addScopeVariableDeclaration(decl, symID)
			}
			result = append(result, decl)
		} else {
			initStmt := lua.Assign(
				[]lua.Expression{classNameIdent},
				[]lua.Expression{classInitExpr},
			)
			t.setNodePos(initStmt, node)
			result = append(result, initStmt)
		}

		var classRef lua.Expression = lua.Ident(name)
		if isExported && !t.isExportAsGlobalTopLevel() {
			exportRef := lua.Index(lua.Ident(exportTarget), lua.Str(exportKey))
			result = append(result, lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(name)},
				[]lua.Expression{exportRef},
			))
			classRef = lua.Ident(name)
		}

		if !cs.isTSTL() {
			nameStmt := lua.Assign(
				[]lua.Expression{lua.Index(classRef, lua.Str("__name"))},
				[]lua.Expression{lua.Str(origName)},
			)
			t.setNodePos(nameStmt, node)
			result = append(result, nameStmt)
		} else {
			nameStmt := lua.Assign(
				[]lua.Expression{memberAccess(classRef, "name")},
				[]lua.Expression{lua.Str(origName)},
			)
			t.setNodePos(nameStmt, node)
			result = append(result, nameStmt)
			if baseExpr != nil {
				classExtends := t.requireLualib("__TS__ClassExtends")
				extendsStmt := lua.ExprStmt(lua.Call(lua.Ident(classExtends), classRef, baseExpr))
				// Position extends call at the "extends" keyword in the heritage clause
				if cd.HeritageClauses != nil {
					for _, hclause := range cd.HeritageClauses.Nodes {
						if hclause.AsHeritageClause().Token == ast.KindExtendsKeyword {
							t.setNodePos(extendsStmt, hclause)
							break
						}
					}
				}
				result = append(result, extendsStmt)
			}
		}

		prevBaseClassName := t.currentBaseClassName
		prevClassRef := t.currentClassRef
		t.currentClassRef = classRef
		t.currentBaseClassName = t.getBaseClassName(cd.HeritageClauses)

		result = append(result, t.transformClassMembersShared(node, classRef)...)

		t.currentBaseClassName = prevBaseClassName
		t.currentClassRef = prevClassRef

		if ast.HasDecorators(node) {
			decoratingExpr := t.createClassDecoratingExpression(node, classRef, origName)
			result = append(result, lua.Assign(
				[]lua.Expression{classRef},
				[]lua.Expression{decoratingExpr},
			))
			if isExported {
				eKey := name
				if isDefault {
					eKey = "default"
				}
				if !t.isExportAsGlobalTopLevel() {
					result = append(result, lua.Assign(
						[]lua.Expression{lua.Index(lua.Ident(exportTarget), lua.Str(eKey))},
						[]lua.Expression{classRef},
					))
				}
			}
		}
	}

	if len(comments) > 0 && len(result) > 0 {
		setLeadingComments(result[0], comments)
	}

	return result
}

// getBaseClassName extracts the base class identifier name from heritage clauses, if it's a simple identifier.
func (t *Transpiler) getBaseClassName(heritageClauses *ast.NodeList) string {
	if heritageClauses == nil {
		return ""
	}
	for _, clause := range heritageClauses.Nodes {
		hc := clause.AsHeritageClause()
		if hc.Token == ast.KindExtendsKeyword && hc.Types != nil && len(hc.Types.Nodes) > 0 {
			expr := hc.Types.Nodes[0].AsExpressionWithTypeArguments().Expression
			if expr.Kind == ast.KindIdentifier {
				// If the base class is exported (namespace or module), use ____super
				// since the direct identifier may not be in scope
				if t.getIdentifierExportScope(expr) != nil {
					return ""
				}
				// Also check for top-level module exports
				sym := t.checker.GetSymbolAtLocation(expr)
				if sym != nil {
					for _, decl := range sym.Declarations {
						if hasExportModifier(decl) {
							return ""
						}
					}
				}
				name := expr.AsIdentifier().Text
				if t.hasUnsafeIdentifierName(expr) {
					return luaSafeName(name)
				}
				return name
			}
		}
	}
	return ""
}

func (t *Transpiler) transformClassMembersShared(node *ast.Node, classRef lua.Expression) []lua.Statement {
	var members *ast.NodeList
	switch node.Kind {
	case ast.KindClassDeclaration:
		members = node.AsClassDeclaration().Members
	case ast.KindClassExpression:
		members = node.AsClassExpression().Members
	default:
		return nil
	}
	if members == nil {
		return nil
	}

	var constructor *ast.Node
	var instanceFields []*ast.Node
	var methods []*ast.Node

	for _, member := range members.Nodes {
		// Private identifiers (#x) are not supported
		if isPrivateClassMember(member) {
			t.addError(member, dw.UnsupportedProperty, "Private identifiers are not supported.")
			continue
		}
		switch member.Kind {
		case ast.KindConstructor:
			constructor = member
		case ast.KindPropertyDeclaration:
			if !hasStaticModifier(member) {
				instanceFields = append(instanceFields, member)
			}
			// Static fields are processed later in declaration order with static blocks
		case ast.KindMethodDeclaration:
			methods = append(methods, member)
		case ast.KindGetAccessor, ast.KindSetAccessor:
			methods = append(methods, member)
		case ast.KindClassStaticBlockDeclaration:
			// Processed later in declaration order with static fields
		case ast.KindSemicolonClassElement:
			// skip
		default:
			// skip unknown
		}
	}

	var result []lua.Statement

	// Constructor
	var heritageClauses *ast.NodeList
	switch node.Kind {
	case ast.KindClassDeclaration:
		heritageClauses = node.AsClassDeclaration().HeritageClauses
	case ast.KindClassExpression:
		heritageClauses = node.AsClassExpression().HeritageClauses
	}
	hasBase := false
	if heritageClauses != nil {
		for _, clause := range heritageClauses.Nodes {
			if clause.AsHeritageClause().Token == ast.KindExtendsKeyword {
				hasBase = true
				break
			}
		}
	}

	// TSTL omits the constructor entirely when a subclass has no explicit constructor
	// and no instance fields with initializers — the runtime handles inheritance.
	hasInstanceFieldInits := false
	for _, field := range instanceFields {
		if field.AsPropertyDeclaration().Initializer != nil {
			hasInstanceFieldInits = true
			break
		}
	}
	if constructor != nil || hasInstanceFieldInits || !hasBase {
		ctorStmts := t.transformClassConstructor(classRef, constructor, instanceFields, hasBase)
		// Set source position: explicit constructor maps to the constructor node,
		// implicit (generated) constructor maps to the class declaration node.
		if len(ctorStmts) > 0 {
			posNode := node // class declaration
			if constructor != nil {
				posNode = constructor
			}
			t.setNodePos(ctorStmts[0].(lua.Positioned), posNode)
		}
		result = append(result, ctorStmts...)
	}

	// Legacy constructor parameter decorators
	if constructor != nil && t.compilerOptions != nil && t.compilerOptions.ExperimentalDecorators.IsTrue() {
		if stmt := t.createConstructorDecoratingExpression(constructor, classRef); stmt != nil {
			result = append(result, stmt)
		}
	}

	// First pass: methods only (so static properties can call them)
	for _, method := range methods {
		if method.Kind == ast.KindMethodDeclaration {
			result = append(result, t.transformClassMethod(classRef, method)...)
		}
	}

	// Second pass: accessors, static fields, static blocks, and property decorators
	// — all interleaved in declaration order.
	//
	// For accessors, we pair getters/setters by name and only emit when we
	// encounter the first accessor of the pair (like TSTL's getAllAccessorDeclarations).
	type accessorPair struct {
		getter *ast.Node
		setter *ast.Node
		first  *ast.Node
	}
	accessorPairs := make(map[string]*accessorPair)
	// Pre-scan to build accessor pairs
	for _, member := range members.Nodes {
		if member.Kind != ast.KindGetAccessor && member.Kind != ast.KindSetAccessor {
			continue
		}
		var name string
		if member.Kind == ast.KindGetAccessor {
			name = member.AsGetAccessorDeclaration().Name().AsIdentifier().Text
		} else {
			name = member.AsSetAccessorDeclaration().Name().AsIdentifier().Text
		}
		key := name
		if hasStaticModifier(member) {
			key = "static:" + name
		}
		pair, exists := accessorPairs[key]
		if !exists {
			pair = &accessorPair{first: member}
			accessorPairs[key] = pair
		}
		if member.Kind == ast.KindGetAccessor {
			pair.getter = member
		} else {
			pair.setter = member
		}
	}

	for _, member := range members.Nodes {
		switch member.Kind {
		case ast.KindGetAccessor, ast.KindSetAccessor:
			var name string
			if member.Kind == ast.KindGetAccessor {
				name = member.AsGetAccessorDeclaration().Name().AsIdentifier().Text
			} else {
				name = member.AsSetAccessorDeclaration().Name().AsIdentifier().Text
			}
			key := name
			if hasStaticModifier(member) {
				key = "static:" + name
			}
			pair := accessorPairs[key]
			// Only emit on the first accessor of the pair
			if pair.first == member {
				result = append(result, t.transformAccessorPair(classRef, pair.getter, pair.setter)...)
			}
		case ast.KindPropertyDeclaration:
			if hasStaticModifier(member) {
				result = append(result, t.transformStaticField(classRef, member)...)
			}
			// Property decorators (TC39 or legacy)
			if len(getDecorators(member)) > 0 {
				result = append(result, lua.ExprStmt(
					t.createClassPropertyDecoratingExpression(member, classRef),
				))
			}
		case ast.KindClassStaticBlockDeclaration:
			sb := member.AsClassStaticBlockDeclaration()
			bodyStmts := t.transformBlock(sb.Body.AsNode())
			fn := &lua.FunctionExpression{
				Params: []*lua.Identifier{lua.Ident("self")},
				Body:   &lua.Block{Statements: bodyStmts},
			}
			result = append(result, lua.ExprStmt(lua.Call(fn, classRef)))
		}
	}

	return result
}

func (t *Transpiler) transformClassConstructor(classRef lua.Expression, constructor *ast.Node, instanceFields []*ast.Node, hasBase bool) []lua.Statement {
	fnTarget := t.classStyle.constructorAccess(classRef)

	var paramIdents []*lua.Identifier
	paramIdents = append(paramIdents, lua.Ident("self"))
	var hasRest bool

	if constructor != nil {
		ctor := constructor.AsConstructorDeclaration()
		if ctor.Parameters != nil {
			for _, p := range ctor.Parameters.Nodes {
				param := p.AsParameterDeclaration()
				name := t.paramName(param.Name())
				if name == "this" {
					continue
				}
				if param.DotDotDotToken != nil {
					hasRest = true
					continue
				}
				paramIdents = append(paramIdents, lua.Ident(name))
			}
		}
	} else if hasBase {
		hasRest = true
	}

	// Collect parameter property assignments (public/private/protected/readonly params)
	type paramProp struct {
		origName string // original TS name (for self.origName)
		safeName string // possibly renamed Lua identifier
	}
	var paramProps []paramProp
	if constructor != nil {
		ctor := constructor.AsConstructorDeclaration()
		if ctor.Parameters != nil {
			for _, p := range ctor.Parameters.Nodes {
				param := p.AsParameterDeclaration()
				if ast.HasSyntacticModifier(p, ast.ModifierFlagsParameterPropertyModifier) {
					if param.Name().Kind == ast.KindIdentifier {
						origName := param.Name().AsIdentifier().Text
						safeName := origName
						if t.hasUnsafeIdentifierName(param.Name()) {
							safeName = luaSafeName(origName)
						}
						paramProps = append(paramProps, paramProp{origName, safeName})
					}
				}
			}
		}
	}

	var bodyStmts []lua.Statement

	// If extends and no explicit constructor, call super using base class name when available
	if constructor == nil && hasBase {
		var spreadArgs lua.Expression
		if t.luaTarget.HasVarargDots() {
			spreadArgs = lua.Dots()
		} else {
			spreadArgs = lua.Call(t.unpackIdent(), lua.Ident("arg"))
		}
		var superFn lua.Expression
		if t.currentBaseClassName != "" {
			superFn = memberAccess(lua.Ident(t.currentBaseClassName), "prototype", "____constructor")
		} else {
			superFn = memberAccess(classRef, "____super", "prototype", "____constructor")
		}
		bodyStmts = append(bodyStmts, lua.ExprStmt(
			lua.Call(superFn, lua.Ident("self"), spreadArgs),
		))
	}

	// Constructor: param defaults, super call, param properties, field inits, body
	if constructor != nil {
		ctor := constructor.AsConstructorDeclaration()
		t.computeOptimizedVarArgs(ctor.Parameters, ctor.Body, false)
		bodyStmts = append(bodyStmts, t.transformParamPreamble(ctor.Parameters)...)

		// Transform the constructor body and split out the super call
		var ctorBodyStmts []lua.Statement
		if ctor.Body != nil {
			ctorBodyStmts = t.transformBlock(ctor.Body)
		}

		// Extract super call from the body (must run before field initializers)
		superIdx := -1
		if hasBase {
			for i, stmt := range ctorBodyStmts {
				if t.isSuperCallStatement(stmt) {
					superIdx = i
					break
				}
			}
		}

		if superIdx >= 0 {
			// Emit statements up to and including the super call
			bodyStmts = append(bodyStmts, ctorBodyStmts[:superIdx+1]...)
			ctorBodyStmts = ctorBodyStmts[superIdx+1:]
		}

		// Order depends on useDefineForClassFields:
		// - true  (ES2022+): field initializers first, then param properties ([[Define]] semantics)
		// - false (legacy):  param properties first, then field initializers
		if t.useDefineForClassFields() {
			for _, field := range instanceFields {
				pd := field.AsPropertyDeclaration()
				if pd.Initializer != nil {
					fieldKey := t.propertyKey(pd.Name())
					init, prec := t.transformExprInScope(pd.Initializer)
					bodyStmts = append(bodyStmts, prec...)
					bodyStmts = append(bodyStmts, lua.Assign(
						[]lua.Expression{lua.Index(lua.Ident("self"), fieldKey)},
						[]lua.Expression{init},
					))
				}
			}
			for _, pp := range paramProps {
				bodyStmts = append(bodyStmts, lua.Assign(
					[]lua.Expression{lua.Index(lua.Ident("self"), lua.Str(pp.origName))},
					[]lua.Expression{lua.Ident(pp.safeName)},
				))
			}
		} else {
			for _, pp := range paramProps {
				bodyStmts = append(bodyStmts, lua.Assign(
					[]lua.Expression{lua.Index(lua.Ident("self"), lua.Str(pp.origName))},
					[]lua.Expression{lua.Ident(pp.safeName)},
				))
			}
			for _, field := range instanceFields {
				pd := field.AsPropertyDeclaration()
				if pd.Initializer != nil {
					fieldKey := t.propertyKey(pd.Name())
					init, prec := t.transformExprInScope(pd.Initializer)
					bodyStmts = append(bodyStmts, prec...)
					bodyStmts = append(bodyStmts, lua.Assign(
						[]lua.Expression{lua.Index(lua.Ident("self"), fieldKey)},
						[]lua.Expression{init},
					))
				}
			}
		}

		// Remaining constructor body after super call
		bodyStmts = append(bodyStmts, ctorBodyStmts...)
	} else {
		// No explicit constructor — super call (if base) + instance field initializers
		for _, field := range instanceFields {
			pd := field.AsPropertyDeclaration()
			if pd.Initializer != nil {
				fieldKey := t.propertyKey(pd.Name())
				init, prec := t.transformExprInScope(pd.Initializer)
				bodyStmts = append(bodyStmts, prec...)
				bodyStmts = append(bodyStmts, lua.Assign(
					[]lua.Expression{lua.Index(lua.Ident("self"), fieldKey)},
					[]lua.Expression{init},
				))
			}
		}
	}

	fn := &lua.FunctionExpression{
		Params: paramIdents,
		Dots:   hasRest,
		Body:   &lua.Block{Statements: bodyStmts},
		Flags:  lua.FlagDeclaration,
	}

	return []lua.Statement{lua.Assign(
		[]lua.Expression{fnTarget},
		[]lua.Expression{fn},
	)}
}

// isSuperCallStatement checks if a Lua statement is a super constructor call.
// Matches patterns like: ClassName.____super.prototype.____constructor(self, ...)
func (t *Transpiler) isSuperCallStatement(stmt lua.Statement) bool {
	es, ok := stmt.(*lua.ExpressionStatement)
	if !ok {
		return false
	}
	call, ok := es.Expression.(*lua.CallExpression)
	if !ok {
		return false
	}
	// Check if the function being called ends with .____constructor
	idx, ok := call.Expression.(*lua.TableIndexExpression)
	if !ok {
		return false
	}
	key, ok := idx.Index.(*lua.StringLiteral)
	if !ok {
		return false
	}
	return key.Value == "____constructor"
}

func (t *Transpiler) transformClassMethod(classRef lua.Expression, node *ast.Node) []lua.Statement {
	switch node.Kind {
	case ast.KindMethodDeclaration:
		md := node.AsMethodDeclaration()
		// Skip overload declarations (no body) — only emit the implementation
		if md.Body == nil {
			return nil
		}
		mnExpr := t.propertyKey(md.Name())
		// toString → __tostring (Lua metamethod)
		if str, ok := mnExpr.(*lua.StringLiteral); ok && str.Value == "toString" {
			mnExpr = lua.Str("__tostring")
		}
		var mnPrec []lua.Statement
		needsSelf := t.functionNeedsSelf(node)
		paramIdents, hasRest := t.transformParamIdents(md.Parameters, needsSelf)

		var target lua.Expression
		if hasStaticModifier(node) {
			target = lua.Index(classRef, mnExpr)
		} else {
			target = lua.Index(t.classStyle.methodTarget(classRef), mnExpr)
		}

		isAsync := hasAsyncModifier(node)
		if isAsync {
			t.asyncDepth++
		}

		isGenerator := md.AsteriskToken != nil
		if isGenerator {
			t.generatorDepth++
		}

		isStatic := hasStaticModifier(node)
		prevInStaticMethod := t.inStaticMethod
		t.inStaticMethod = isStatic

		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, t.transformParamPreamble(md.Parameters)...)
		if md.Body != nil {
			bodyStmts = append(bodyStmts, t.transformBlock(md.Body)...)
		}

		t.inStaticMethod = prevInStaticMethod

		if isAsync {
			bodyStmts = t.wrapInAsyncAwaiter(bodyStmts)
			t.asyncDepth--
		}

		if isGenerator {
			t.generatorDepth--
		}

		fnExpr := lua.Expression(&lua.FunctionExpression{
			Params: paramIdents,
			Dots:   hasRest,
			Body:   &lua.Block{Statements: bodyStmts},
			Flags:  lua.FlagDeclaration,
		})

		// Wrap generator methods in __TS__Generator
		if isGenerator {
			genFn := t.requireLualib("__TS__Generator")
			fnExpr = lua.Call(lua.Ident(genFn), fnExpr)
		}
		fn := fnExpr

		methodDecorators := getDecorators(node)
		hasParamDecorators := t.hasParameterDecorators(node)
		if len(methodDecorators) > 0 || hasParamDecorators {
			if t.compilerOptions != nil && t.compilerOptions.ExperimentalDecorators.IsTrue() {
				// Legacy: assign method then decorate
				result := append(mnPrec, lua.Assign(
					[]lua.Expression{target},
					[]lua.Expression{fn},
				))
				result = append(result, lua.ExprStmt(
					t.createClassMethodDecoratingExpression(node, fn, classRef),
				))
				return result
			}
			// TC39: method = __TS__Decorate(...)
			decorated := t.createClassMethodDecoratingExpression(node, fn, classRef)
			result := append(mnPrec, lua.Assign(
				[]lua.Expression{target},
				[]lua.Expression{decorated},
			))
			return result
		}

		result := append(mnPrec, lua.Assign(
			[]lua.Expression{target},
			[]lua.Expression{fn},
		))
		return result

	case ast.KindGetAccessor:
		ga := node.AsGetAccessorDeclaration()
		gnExpr := t.propertyKey(ga.Name())
		var gnPrec []lua.Statement
		isStatic := hasStaticModifier(node)

		var descriptorTarget lua.Expression
		if isStatic {
			descriptorTarget = classRef
		} else {
			descriptorTarget = memberAccess(classRef, "prototype")
		}

		var bodyStmts []lua.Statement
		if ga.Body != nil {
			bodyStmts = t.transformBlock(ga.Body)
		}

		getterFn := &lua.FunctionExpression{
			Params: []*lua.Identifier{lua.Ident("self")},
			Body:   &lua.Block{Statements: bodyStmts},
		}

		var getterValue lua.Expression = getterFn
		if len(getDecorators(node)) > 0 {
			getterValue = t.createClassAccessorDecoratingExpression(node, getterFn, classRef)
		}

		descriptor := lua.Table(lua.KeyField(lua.Ident("get"), getterValue))
		if isStatic {
			fn := t.requireLualib("__TS__ObjectDefineProperty")
			result := append(gnPrec, lua.ExprStmt(lua.Call(
				lua.Ident(fn), descriptorTarget, gnExpr, descriptor,
			)))
			return result
		}
		fn := t.requireLualib("__TS__SetDescriptor")
		result := append(gnPrec, lua.ExprStmt(lua.Call(
			lua.Ident(fn), descriptorTarget, gnExpr, descriptor, lua.Bool(true),
		)))
		return result

	case ast.KindSetAccessor:
		sa := node.AsSetAccessorDeclaration()
		snExpr := t.propertyKey(sa.Name())
		var snPrec []lua.Statement
		isStatic := hasStaticModifier(node)

		var descriptorTarget lua.Expression
		if isStatic {
			descriptorTarget = classRef
		} else {
			descriptorTarget = memberAccess(classRef, "prototype")
		}

		paramIdents, hasRest := t.transformParamIdents(sa.Parameters, true)
		var bodyStmts []lua.Statement
		if sa.Body != nil {
			bodyStmts = t.transformBlock(sa.Body)
		}

		setterFn := &lua.FunctionExpression{
			Params: paramIdents,
			Dots:   hasRest,
			Body:   &lua.Block{Statements: bodyStmts},
		}

		var setterValue lua.Expression = setterFn
		if len(getDecorators(node)) > 0 {
			setterValue = t.createClassAccessorDecoratingExpression(node, setterFn, classRef)
		}

		descriptor := lua.Table(lua.KeyField(lua.Ident("set"), setterValue))
		if isStatic {
			fn := t.requireLualib("__TS__ObjectDefineProperty")
			result := append(snPrec, lua.ExprStmt(lua.Call(
				lua.Ident(fn), descriptorTarget, snExpr, descriptor,
			)))
			return result
		}
		fn := t.requireLualib("__TS__SetDescriptor")
		result := append(snPrec, lua.ExprStmt(lua.Call(
			lua.Ident(fn), descriptorTarget, snExpr, descriptor, lua.Bool(true),
		)))
		return result
	}
	return nil
}

// transformAccessorPair merges getter and setter for the same property into one __TS__SetDescriptor call.
func (t *Transpiler) transformAccessorPair(classRef lua.Expression, getter, setter *ast.Node) []lua.Statement {
	if getter != nil && setter == nil {
		return t.transformClassMethod(classRef, getter)
	}
	if getter == nil && setter != nil {
		return t.transformClassMethod(classRef, setter)
	}

	// Both getter and setter exist — merge into one call
	ga := getter.AsGetAccessorDeclaration()
	sa := setter.AsSetAccessorDeclaration()
	gnExpr := t.propertyKey(ga.Name())
	isStatic := hasStaticModifier(getter)

	var descriptorTarget lua.Expression
	if isStatic {
		descriptorTarget = classRef
	} else {
		descriptorTarget = memberAccess(classRef, "prototype")
	}

	var getBodyStmts []lua.Statement
	if ga.Body != nil {
		getBodyStmts = t.transformBlock(ga.Body)
	}
	getterFn := &lua.FunctionExpression{
		Params: []*lua.Identifier{lua.Ident("self")},
		Body:   &lua.Block{Statements: getBodyStmts},
	}

	setParamIdents, hasRest := t.transformParamIdents(sa.Parameters, true)
	var setBodyStmts []lua.Statement
	if sa.Body != nil {
		setBodyStmts = t.transformBlock(sa.Body)
	}
	setterFn := &lua.FunctionExpression{
		Params: setParamIdents,
		Dots:   hasRest,
		Body:   &lua.Block{Statements: setBodyStmts},
	}

	// Static uses __TS__ObjectDefineProperty (3 args), instance uses __TS__SetDescriptor (4 args, with true)
	descriptor := lua.Table(
		lua.KeyField(lua.Ident("get"), getterFn),
		lua.KeyField(lua.Ident("set"), setterFn),
	)
	if isStatic {
		fn := t.requireLualib("__TS__ObjectDefineProperty")
		return []lua.Statement{lua.ExprStmt(lua.Call(
			lua.Ident(fn), descriptorTarget, gnExpr, descriptor,
		))}
	}
	fn := t.requireLualib("__TS__SetDescriptor")
	return []lua.Statement{lua.ExprStmt(lua.Call(
		lua.Ident(fn), descriptorTarget, gnExpr, descriptor, lua.Bool(true),
	))}
}

func (t *Transpiler) transformStaticField(classRef lua.Expression, node *ast.Node) []lua.Statement {
	pd := node.AsPropertyDeclaration()
	if pd.Initializer == nil {
		// TSTL omits static fields without initializers
		return nil
	}
	fieldKey := t.propertyKey(pd.Name())
	target := lua.Index(classRef, fieldKey)
	init, prec := t.transformExprInScope(pd.Initializer)
	result := make([]lua.Statement, 0, len(prec)+1)
	result = append(result, prec...)
	result = append(result, lua.Assign(
		[]lua.Expression{target},
		[]lua.Expression{init},
	))
	return result
}

// isArrayConstructor checks if a new expression targets the standard Array constructor.
// Uses the type checker when available to verify the symbol, falling back to name check.
func (t *Transpiler) isArrayConstructor(ne *ast.NewExpression) bool {
	if ne.Expression.Kind != ast.KindIdentifier || ne.Expression.AsIdentifier().Text != "Array" {
		return false
	}

	typ := t.checker.GetTypeAtLocation(ne.Expression)
	if typ == nil {
		return true
	}
	sym := typ.Symbol()
	if sym == nil {
		return true
	}
	return sym.Name == "ArrayConstructor"
}

func (t *Transpiler) transformNewExpression(node *ast.Node) lua.Expression {
	ne := node.AsNewExpression()

	// Language extension: new LuaTable() → {}
	if t.isTableNewCall(ne) {
		return lua.Table()
	}

	// new Array() → {}, new Array(a,b,c) → {a,b,c}, new Array(n) → diagnostic + {}
	// Use symbol check when available to avoid false positives on shadowed Array.
	if t.isArrayConstructor(ne) {
		if ne.Arguments == nil || len(ne.Arguments.Nodes) == 0 {
			return lua.Table()
		}
		// Check if this is the length constructor (single param, no rest parameter)
		sig := t.checker.GetResolvedSignature(node)
		if sig != nil {
			sigDecl := checker.Signature_declaration(sig)
			if sigDecl != nil {
				params := sigDecl.Parameters()
				if len(params) == 1 && params[0].AsParameterDeclaration().DotDotDotToken == nil {
					t.addError(node, dw.UnsupportedArrayWithLengthConstructor, "Constructing new Array with length is not supported.")
					return lua.Table()
				}
			}
		}
		argExprs := t.transformArgExprs(ne.Arguments)
		if len(argExprs) == 1 {
			// Fallback for no checker: still produce empty table
			return lua.Table()
		}
		// new Array(a, b, c) → {a, b, c}
		var fields []*lua.TableFieldExpression
		for _, arg := range argExprs {
			fields = append(fields, lua.Field(arg))
		}
		return lua.Table(fields...)
	}

	if ne.Expression.Kind == ast.KindIdentifier {
		name := ne.Expression.AsIdentifier().Text
		if isLualibConstructor(name) {
			t.requireLualib(name)
		}
	}

	// @customConstructor: new Class(args) → CustomCreate(args)
	if customCtor := t.getTypeAnnotationArg(ne.Expression, "customconstructor"); customCtor != "" {
		argExprs := t.transformArgExprs(ne.Arguments)
		return lua.Call(lua.Ident(customCtor), argExprs...)
	} else if t.hasTypeAnnotationTag(ne.Expression, "customconstructor") {
		// Annotation exists but has no argument (or empty argument)
		t.addError(node, dw.AnnotationInvalidArgumentCount,
			"'@customConstructor' expects 1 arguments, but got 0.")
	}

	fn := t.transformExpression(ne.Expression)

	// Transform args in scope to preserve evaluation order
	argExprs, argPrec := t.transformArgsInScope(ne.Arguments)
	if len(argPrec) > 0 {
		fn = t.moveToPrecedingTemp(fn)
		t.addPrecedingStatements(argPrec...)
	}

	// Alternative class style: direct call or method-new
	if newExpr := t.classStyle.newExpr(fn, argExprs); newExpr != nil {
		return newExpr
	}

	// TSTL default: __TS__New(classExpr, args...)
	newFn := t.requireLualib("__TS__New")
	params := append([]lua.Expression{fn}, argExprs...)
	return lua.Call(lua.Ident(newFn), params...)
}

func isLualibConstructor(name string) bool {
	switch name {
	case "Set", "Map", "WeakMap", "WeakSet":
		return true
	}
	return false
}

func isPrivateClassMember(member *ast.Node) bool {
	switch member.Kind {
	case ast.KindPropertyDeclaration:
		return member.AsPropertyDeclaration().Name().Kind == ast.KindPrivateIdentifier
	case ast.KindMethodDeclaration:
		return member.AsMethodDeclaration().Name().Kind == ast.KindPrivateIdentifier
	case ast.KindGetAccessor:
		return member.AsGetAccessorDeclaration().Name().Kind == ast.KindPrivateIdentifier
	case ast.KindSetAccessor:
		return member.AsSetAccessorDeclaration().Name().Kind == ast.KindPrivateIdentifier
	}
	return false
}

func hasStaticModifier(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, m := range modifiers.Nodes {
		if m.Kind == ast.KindStaticKeyword {
			return true
		}
	}
	return false
}

func (t *Transpiler) transformSuperCall(node *ast.Node) lua.Expression {
	ce := node.AsCallExpression()
	argExprs := t.transformArgExprs(ce.Arguments)
	// Use base class name directly if available, otherwise fall back to self.____super
	var base lua.Expression
	if t.currentBaseClassName != "" {
		base = lua.Ident(t.currentBaseClassName)
	} else if t.currentClassRef != nil {
		base = lua.Index(t.currentClassRef, lua.Str("____super"))
	} else {
		// No enclosing class — emit ____.____constructor(self) like TSTL
		ctorName := t.classStyle.constructorName()
		fn := memberAccess(lua.Ident("____"), ctorName)
		params := append([]lua.Expression{lua.Ident("self")}, argExprs...)
		return lua.Call(fn, params...)
	}

	// Alternative class style
	if superExpr := t.classStyle.superConstructorCall(base, t.currentClassRef, argExprs); superExpr != nil {
		return superExpr
	}

	// TSTL default: Base.prototype.____constructor(self, args)
	fn := memberAccess(base, "prototype", "____constructor")
	params := append([]lua.Expression{lua.Ident("self")}, argExprs...)
	return lua.Call(fn, params...)
}

func isSuperCall(node *ast.Node) bool {
	if node.Kind != ast.KindCallExpression {
		return false
	}
	ce := node.AsCallExpression()
	return ce.Expression.Kind == ast.KindSuperKeyword
}

func isSuperMethodCall(node *ast.Node) bool {
	if node.Kind != ast.KindCallExpression {
		return false
	}
	ce := node.AsCallExpression()
	if ce.Expression.Kind == ast.KindPropertyAccessExpression {
		pa := ce.Expression.AsPropertyAccessExpression()
		return pa.Expression.Kind == ast.KindSuperKeyword
	}
	return false
}

func (t *Transpiler) transformSuperMethodCall(node *ast.Node) lua.Expression {
	ce := node.AsCallExpression()
	pa := ce.Expression.AsPropertyAccessExpression()
	method := pa.Name().AsIdentifier().Text
	argExprs := t.transformArgExprs(ce.Arguments)
	var base lua.Expression
	if t.currentBaseClassName != "" {
		base = lua.Ident(t.currentBaseClassName)
	} else if t.currentClassRef != nil {
		base = lua.Index(t.currentClassRef, lua.Str("____super"))
	} else {
		base = memberAccess(lua.Ident("self"), "____super")
	}

	// Alternative class style
	if superExpr := t.classStyle.superMethodCall(base, t.currentClassRef, method, t.inStaticMethod, argExprs); superExpr != nil {
		return superExpr
	}

	// TSTL default
	var fn lua.Expression
	if t.inStaticMethod {
		fn = memberAccess(base, method)
	} else {
		fn = memberAccess(base, "prototype", method)
	}
	params := append([]lua.Expression{lua.Ident("self")}, argExprs...)
	return lua.Call(fn, params...)
}
