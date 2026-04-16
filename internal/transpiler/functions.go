package transpiler

import (
	"fmt"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/realcoldfry/tslua/internal/lua"
)

// isFunctionTypeWithProperties checks whether a type is a function type that also has
// user-defined properties (e.g. `function foo() {}; foo.bar = "baz";`).
func (t *Transpiler) isFunctionTypeWithProperties(typ *checker.Type) bool {
	if typ == nil {
		return false
	}
	flags := checker.Type_flags(typ)
	if flags&checker.TypeFlagsUnion != 0 {
		for _, member := range typ.Types() {
			if t.isFunctionTypeWithProperties(member) {
				return true
			}
		}
		return false
	}
	// Check if it's a function type (has call signatures)
	callSigs := checker.Checker_getSignaturesOfType(t.checker, typ, checker.SignatureKindCall)
	if len(callSigs) == 0 {
		return false
	}
	// Check if it has properties beyond the call signatures
	props := checker.Checker_getPropertiesOfType(t.checker, typ)
	return len(props) > 0
}

// createCallableTable wraps a function expression in setmetatable({}, {__call = fn}).
// The __call metamethod receives the table as its first argument, so we prepend a dummy parameter.
func createCallableTable(fnExpr lua.Expression) lua.Expression {
	if fe, ok := fnExpr.(*lua.FunctionExpression); ok {
		// Prepend an anonymous dummy parameter to eat the table arg from __call
		fe.Params = append([]*lua.Identifier{lua.Ident("____")}, fe.Params...)
	} else {
		// Wrap in a forwarding function: function(____, ...) return fn(...) end
		fnExpr = &lua.FunctionExpression{
			Params: []*lua.Identifier{lua.Ident("____")},
			Dots:   true,
			Body: &lua.Block{Statements: []lua.Statement{
				lua.Return(lua.Call(fnExpr, lua.Dots())),
			}},
			Flags: lua.FlagInline,
		}
	}
	return lua.Call(
		lua.Ident("setmetatable"),
		lua.Table(),
		lua.Table(lua.KeyField(lua.Str("__call"), fnExpr)),
	)
}

func (t *Transpiler) transformFunctionDeclaration(node *ast.Node) []lua.Statement {
	fd := node.AsFunctionDeclaration()
	// Skip overload declarations (no body) — only emit the implementation
	if fd.Body == nil {
		return nil
	}
	origName := ""
	name := ""
	if fd.Name() != nil {
		origName = fd.Name().AsIdentifier().Text
		name = origName
		// Check @customName annotation
		if customName := t.getCustomName(node); customName != "" {
			name = customName
			origName = customName
		}
		if t.hasUnsafeIdentifierName(fd.Name()) {
			name = luaSafeName(name)
		}
	}

	isExported := hasExportModifier(node)
	needsSelf := t.functionNeedsSelf(node)
	paramIdents, hasRest := t.transformParamIdents(fd.Parameters, needsSelf)

	isAsync := hasAsyncModifier(node)
	isGenerator := fd.AsteriskToken != nil

	// Save and reset function-scoped depths. Each function boundary starts
	// a fresh context: return statements in nested non-async functions must
	// not get ____awaiter_resolve wrapping from a parent async function.
	savedAsyncDepth := t.asyncDepth
	savedTryDepth := t.tryDepth
	if isAsync {
		t.asyncDepth = 1
	} else {
		t.asyncDepth = 0
	}
	t.tryDepth = 0
	if isGenerator {
		t.generatorDepth++
	}

	// Pre-scan: check if rest param can be optimized (spread-only usage)
	if fd.Body != nil {
		t.computeOptimizedVarArgs(fd.Parameters, fd.Body, isAsync)
	}

	// Register the function name symbol BEFORE pushing the function scope,
	// so markSymbolDeclared increments in the parent scope. Self-references
	// in the body will also increment the parent scope's count (via trackSymbolReference),
	// enabling hasMultipleReferences to detect self-referencing functions.
	var funcSymID SymbolID
	if fd.Name() != nil && t.inScope() {
		if sym := t.checker.GetSymbolAtLocation(fd.Name()); sym != nil {
			funcSymID = t.getOrCreateSymbolID(sym)
			t.markSymbolDeclared(funcSymID)
		}
	}

	// Push function scope to capture referenced symbols for hoisting
	funcScope := t.pushScope(ScopeFunction, node)

	// Transform body BEFORE preamble — TSTL processes body first so that
	// symbol references from the body are visible when generating binding
	// pattern declarations (enabling hasMultipleReferences split).
	var blockStmts []lua.Statement
	if fd.Body != nil {
		blockStmts = t.transformBlockStatementsOnly(fd.Body)
	}
	blockStmts = t.performHoisting(funcScope, blockStmts)

	preamble := t.transformParamPreamble(fd.Parameters)

	var bodyStmts []lua.Statement
	if isAsync {
		// For async functions, all preamble (default params, rest param capture,
		// destructuring) happens BEFORE the async wrapper. This matches TSTL
		// behavior and ensures default expressions evaluate synchronously.
		// Rest param capture specifically MUST be before because ... is not
		// available inside the coroutine.
		innerStmts := t.wrapInAsyncAwaiter(blockStmts)
		bodyStmts = append(preamble, innerStmts...)
	} else {
		bodyStmts = make([]lua.Statement, 0, len(preamble)+len(blockStmts))
		bodyStmts = append(bodyStmts, preamble...)
		bodyStmts = append(bodyStmts, blockStmts...)
	}

	t.asyncDepth = savedAsyncDepth
	t.tryDepth = savedTryDepth
	if isGenerator {
		t.generatorDepth--
	}

	t.popScope() // pop function scope

	// Register function's referenced symbols in parent scope for hoisting analysis
	if funcSymID != 0 && t.inScope() {
		t.registerFunctionDefinition(funcSymID, funcScope)
	}

	fn := &lua.FunctionExpression{
		Params: paramIdents,
		Dots:   hasRest,
		Body:   &lua.Block{Statements: bodyStmts},
		Flags:  lua.FlagDeclaration,
	}
	t.setNodePos(fn, node)

	// Wrap generator functions in __TS__Generator
	var valueExpr lua.Expression = fn
	if isGenerator {
		genFn := t.requireLualib("__TS__Generator")
		valueExpr = lua.Call(lua.Ident(genFn), fn)
	}
	if fd.Name() != nil {
		typ := t.checker.GetTypeAtLocation(fd.Name())
		if t.isFunctionTypeWithProperties(typ) {
			valueExpr = createCallableTable(valueExpr)
		}
	}

	isDefault := hasDefaultModifier(node)

	var stmt lua.Statement
	if isExported {
		if t.isExportAsGlobalTopLevel() {
			stmt = lua.LocalDecl(
				[]*lua.Identifier{lua.Ident(name)},
				[]lua.Expression{valueExpr},
			)
		} else {
			exportTarget := "____exports"
			if t.currentNamespace != "" {
				exportTarget = t.currentNamespace
			}
			exportKey := origName
			if isDefault {
				exportKey = "default"
			}
			stmt = lua.Assign(
				[]lua.Expression{lua.Index(lua.Ident(exportTarget), lua.Str(exportKey))},
				[]lua.Expression{valueExpr},
			)
		}
	} else if t.shouldUseLocalDeclaration() {
		// Split declaration if the function references itself AND the value was wrapped
		// (callable table or generator), since Lua's `local x = expr` doesn't put x
		// in scope during expr evaluation. Plain function expressions are safe because
		// `local function f() ... f() ... end` handles recursion in Lua.
		// Hoisted functions are split by performHoisting instead.
		isSafeRecursive := (valueExpr == fn) // plain function expression → Lua handles it
		scope := t.peekScope()
		needsSplit := !isSafeRecursive && t.hasMultipleReferences(scope, funcSymID) &&
			!t.shouldHoistSymbol(funcSymID, scope)
		if needsSplit {
			decl := lua.LocalDecl([]*lua.Identifier{lua.Ident(name)}, nil)
			stmt = lua.Assign([]lua.Expression{lua.Ident(name)}, []lua.Expression{valueExpr})
			if funcSymID != 0 && t.inScope() {
				t.setFunctionDefinitionStatement(funcSymID, stmt)
			}
			return []lua.Statement{decl, stmt}
		}
		// Local declaration; hoisting will convert if needed
		stmt = lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(name)},
			[]lua.Expression{valueExpr},
		)
	} else {
		stmt = lua.Assign(
			[]lua.Expression{lua.Ident(name)},
			[]lua.Expression{valueExpr},
		)
	}

	// Register the definition statement for hoisting
	if funcSymID != 0 && t.inScope() {
		t.setFunctionDefinitionStatement(funcSymID, stmt)
	}

	// For `export default function foo()`, create a local alias so the name
	// is still available for self-references and other module-level code.
	if isExported && isDefault && name != "" && !t.exportAsGlobal {
		exportTarget := "____exports"
		if t.currentNamespace != "" {
			exportTarget = t.currentNamespace
		}
		alias := lua.LocalDecl(
			[]*lua.Identifier{lua.Ident(name)},
			[]lua.Expression{lua.Index(lua.Ident(exportTarget), lua.Str("default"))},
		)
		return []lua.Statement{stmt, alias}
	}

	return []lua.Statement{stmt}
}

func (t *Transpiler) transformYieldExpression(node *ast.Node) lua.Expression {
	ye := node.AsYieldExpression()
	var args []lua.Expression
	if ye.Expression != nil {
		args = append(args, t.transformExpression(ye.Expression))
	}
	if ye.AsteriskToken != nil {
		// yield* expr → __TS__DelegatedYield(expr)
		fn := t.requireLualib("__TS__DelegatedYield")
		return lua.Call(lua.Ident(fn), args...)
	}
	// yield expr → coroutine.yield(expr)
	return lua.Call(lua.Index(lua.Ident("coroutine"), lua.Str("yield")), args...)
}

// hasAsyncModifier checks whether a function-like declaration has the `async` keyword.
func hasAsyncModifier(node *ast.Node) bool {
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, m := range modifiers.Nodes {
		if m.Kind == ast.KindAsyncKeyword {
			return true
		}
	}
	return false
}

// wrapInAsyncAwaiter wraps body statements in: return __TS__AsyncAwaiter(function(____awaiter_resolve) ... end)
func (t *Transpiler) wrapInAsyncAwaiter(bodyStmts []lua.Statement) []lua.Statement {
	fn := t.requireLualib("__TS__AsyncAwaiter")
	inner := &lua.FunctionExpression{
		Params: []*lua.Identifier{lua.Ident("____awaiter_resolve")},
		Body:   &lua.Block{Statements: bodyStmts},
	}
	return []lua.Statement{lua.Return(lua.Call(lua.Ident(fn), inner))}
}

// functionNeedsSelf determines whether a function definition needs a self parameter.
// Routes through computeDeclarationContextType for unified self-detection.
func (t *Transpiler) functionNeedsSelf(node *ast.Node) bool {
	return t.getDeclarationContextType(node) != contextVoid
}

type thisParamKind int

const (
	thisParamNone  thisParamKind = iota // no explicit this parameter
	thisParamVoid                       // this: void
	thisParamTyped                      // this: any, this: T, etc.
)

func getThisParamKind(node *ast.Node) thisParamKind {
	params := getFunctionParams(node)
	if params == nil || len(params.Nodes) == 0 {
		return thisParamNone
	}
	firstParam := params.Nodes[0].AsParameterDeclaration()
	if firstParam.Name().Kind != ast.KindIdentifier || firstParam.Name().AsIdentifier().Text != "this" {
		return thisParamNone
	}
	if firstParam.Type != nil && firstParam.Type.Kind == ast.KindVoidKeyword {
		return thisParamVoid
	}
	return thisParamTyped
}

func getFunctionParams(node *ast.Node) *ast.NodeList {
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().Parameters
	case ast.KindArrowFunction:
		return node.AsArrowFunction().Parameters
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().Parameters
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().Parameters
	}
	return nil
}

// getSignatureParams returns parameters from signature/type declaration nodes
// (MethodSignature, CallSignature, FunctionType) that aren't function implementations.
// Used for call-site this: void detection without affecting function definition self handling.
func getSignatureParams(node *ast.Node) *ast.NodeList {
	switch node.Kind {
	case ast.KindMethodSignature:
		return node.AsMethodSignatureDeclaration().Parameters
	case ast.KindCallSignature:
		return node.AsCallSignatureDeclaration().Parameters
	case ast.KindFunctionType:
		return node.AsFunctionTypeNode().Parameters
	}
	return getFunctionParams(node)
}

// getThisParamKindFromSignature checks for this: void in type signatures,
// covering both function implementations and type-level declarations.
func getThisParamKindFromSignature(node *ast.Node) thisParamKind {
	params := getSignatureParams(node)
	if params == nil || len(params.Nodes) == 0 {
		return thisParamNone
	}
	firstParam := params.Nodes[0].AsParameterDeclaration()
	if firstParam.Name().Kind != ast.KindIdentifier || firstParam.Name().AsIdentifier().Text != "this" {
		return thisParamNone
	}
	if firstParam.Type != nil && firstParam.Type.Kind == ast.KindVoidKeyword {
		return thisParamVoid
	}
	return thisParamTyped
}

// Arrow functions become: function(self, params) ... end
func (t *Transpiler) transformArrowFunction(node *ast.Node) lua.Expression {
	af := node.AsArrowFunction()
	// Arrow functions use ____ (unused context) instead of self to avoid shadowing
	var paramIdents []*lua.Identifier
	var hasRest bool
	if t.functionNeedsSelf(node) && len(af.Parameters.Nodes) > 0 {
		// Only add dummy context parameter when arrow has real parameters.
		// Arrow functions with no parameters don't need it.
		paramIdents, hasRest = t.transformParamIdentsWithContextName(af.Parameters, "____")
	} else {
		paramIdents, hasRest = t.transformParamIdents(af.Parameters, false)
	}

	isAsync := hasAsyncModifier(node)
	t.computeOptimizedVarArgs(af.Parameters, af.Body, isAsync)

	// Save and reset function-scoped depths (see transformFunctionDeclaration).
	savedAsyncDepth := t.asyncDepth
	savedTryDepth := t.tryDepth
	if isAsync {
		t.asyncDepth = 1
	} else {
		t.asyncDepth = 0
	}
	t.tryDepth = 0

	if af.Body.Kind == ast.KindBlock {
		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, t.transformParamPreamble(af.Parameters)...)
		bodyStmts = append(bodyStmts, t.transformFunctionBodyBlock(af.Body)...)
		if isAsync {
			bodyStmts = t.wrapInAsyncAwaiter(bodyStmts)
		}
		t.asyncDepth = savedAsyncDepth
		t.tryDepth = savedTryDepth
		return &lua.FunctionExpression{Params: paramIdents, Dots: hasRest, Body: &lua.Block{Statements: bodyStmts}}
	}

	// Expression body: function(self, ...) return <expr> end
	// Use transformExpressionsInReturn to handle $multi() in arrow expression bodies
	if t.hasParamPreamble(af.Parameters) {
		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, t.transformParamPreamble(af.Parameters)...)
		t.pushPrecedingStatements()
		exprs := t.transformExpressionsInReturn(af.Body)
		prec := t.popPrecedingStatements()
		bodyStmts = append(bodyStmts, prec...)
		bodyStmts = append(bodyStmts, lua.Return(exprs...))
		if isAsync {
			bodyStmts = t.wrapInAsyncAwaiter(bodyStmts)
		}
		t.asyncDepth = savedAsyncDepth
		t.tryDepth = savedTryDepth
		return &lua.FunctionExpression{Params: paramIdents, Dots: hasRest, Body: &lua.Block{Statements: bodyStmts}}
	}
	t.pushPrecedingStatements()
	exprs := t.transformExpressionsInReturn(af.Body)
	prec := t.popPrecedingStatements()
	if isAsync {
		var bodyStmts []lua.Statement
		bodyStmts = append(bodyStmts, prec...)
		bodyStmts = append(bodyStmts, lua.Return(exprs...))
		bodyStmts = t.wrapInAsyncAwaiter(bodyStmts)
		t.asyncDepth = savedAsyncDepth
		t.tryDepth = savedTryDepth
		return &lua.FunctionExpression{Params: paramIdents, Dots: hasRest, Body: &lua.Block{Statements: bodyStmts}}
	}
	t.asyncDepth = savedAsyncDepth
	t.tryDepth = savedTryDepth
	if len(prec) > 0 {
		stmts := make([]lua.Statement, 0, len(prec)+1)
		stmts = append(stmts, prec...)
		stmts = append(stmts, lua.Return(exprs...))
		body := &lua.Block{Statements: stmts}
		return &lua.FunctionExpression{Params: paramIdents, Dots: hasRest, Body: body}
	}
	flags := lua.NodeFlags(0)
	if len(exprs) == 1 {
		flags = lua.FlagInline
	}
	body := &lua.Block{Statements: []lua.Statement{
		lua.Return(exprs...),
	}}
	return &lua.FunctionExpression{Params: paramIdents, Dots: hasRest, Body: body, Flags: flags}
}

func (t *Transpiler) transformFunctionExpression(node *ast.Node) lua.Expression {
	fe := node.AsFunctionExpression()
	needsSelf := t.functionNeedsSelf(node)
	paramIdents, hasRest := t.transformParamIdents(fe.Parameters, needsSelf)

	isAsync := hasAsyncModifier(node)
	isGenerator := fe.AsteriskToken != nil

	// Save and reset function-scoped depths (see transformFunctionDeclaration).
	savedAsyncDepth := t.asyncDepth
	savedTryDepth := t.tryDepth
	if isAsync {
		t.asyncDepth = 1
	} else {
		t.asyncDepth = 0
	}
	t.tryDepth = 0
	if isGenerator {
		t.generatorDepth++
	}

	// Pre-scan: check if rest param can be optimized
	if fe.Body != nil {
		t.computeOptimizedVarArgs(fe.Parameters, fe.Body, isAsync)
	}

	// Push function scope to track symbol references for named function expression self-reference
	funcScope := t.pushScope(ScopeFunction, node)

	var result lua.Expression
	if fe.Body != nil {
		// Transform body BEFORE preamble (matching TSTL order)
		blockStmts := t.transformBlockStatementsOnly(fe.Body)
		blockStmts = t.performHoisting(funcScope, blockStmts)
		preamble := t.transformParamPreamble(fe.Parameters)

		bodyStmts := make([]lua.Statement, 0, len(preamble)+len(blockStmts))
		bodyStmts = append(bodyStmts, preamble...)
		bodyStmts = append(bodyStmts, blockStmts...)
		if isAsync {
			bodyStmts = t.wrapInAsyncAwaiter(bodyStmts)
		}
		fe := &lua.FunctionExpression{Params: paramIdents, Dots: hasRest, Body: &lua.Block{Statements: bodyStmts}}
		t.setNodePos(fe, node)
		result = fe
	} else {
		fe := &lua.FunctionExpression{Params: paramIdents, Dots: hasRest}
		t.setNodePos(fe, node)
		result = fe
	}

	t.asyncDepth = savedAsyncDepth
	t.tryDepth = savedTryDepth
	if isGenerator {
		t.generatorDepth--
	}

	// Wrap generator functions in __TS__Generator
	if isGenerator {
		genFn := t.requireLualib("__TS__Generator")
		result = lua.Call(lua.Ident(genFn), result)
	}

	t.popScope()

	// Handle named function expressions that reference themselves
	if fe.Name() != nil {
		name := fe.Name().AsIdentifier().Text
		if t.hasUnsafeIdentifierName(fe.Name()) {
			name = luaSafeName(name)
		}

		typ := t.checker.GetTypeAtLocation(node)
		if t.isFunctionTypeWithProperties(typ) {
			result = createCallableTable(result)
		}

		// Check if the function name is referenced inside the function body
		isReferenced := false
		sym := t.checker.GetSymbolAtLocation(fe.Name())
		if sym != nil {
			symID := t.getSymbolID(sym)
			if symID != 0 && funcScope.ReferencedSymbols != nil && funcScope.ReferencedSymbols[symID] > 0 {
				isReferenced = true
			}
		}

		if isReferenced {
			t.addPrecedingStatements(
				lua.LocalDecl([]*lua.Identifier{lua.Ident(name)}, nil),
				lua.Assign([]lua.Expression{lua.Ident(name)}, []lua.Expression{result}),
			)
			return lua.Ident(name)
		}
	}

	return result
}

// computeOptimizedVarArgs pre-scans a function body to determine if rest params
// can use ... directly for spreads. Marks the param as optimized if it's spread
// and not written to (reads/indexing are fine — table capture handles those).
// If the rest param is modified (e.g., args[i] = x), spreads must use unpack(args).
func (t *Transpiler) computeOptimizedVarArgs(params *ast.NodeList, body *ast.Node, isAsync bool) {
	if params == nil || body == nil || isAsync {
		return
	}
	// Find rest parameter
	var restParam *ast.ParameterDeclaration
	for _, p := range params.Nodes {
		param := p.AsParameterDeclaration()
		if param.DotDotDotToken != nil {
			restParam = param
			break
		}
	}
	if restParam == nil || restParam.Name().Kind != ast.KindIdentifier {
		return
	}
	sym := t.checker.GetSymbolAtLocation(restParam.Name())
	if sym == nil {
		return
	}
	// Check: has spread usage AND is not mutated. If the param is written to
	// (e.g., args[i] = x), we can't use ... for spreads because ... won't
	// reflect mutations made to the captured table.
	if hasVarArgSpreadInScope(body, sym, t.checker) && !isVarArgMutated(body, sym, t.checker) {
		if t.optimizedVarArgs == nil {
			t.optimizedVarArgs = make(map[SymbolID]bool)
		}
		t.optimizedVarArgs[t.getOrCreateSymbolID(sym)] = true
	}
}

// hasVarArgSpreadInScope returns true if any reference to sym in the subtree is inside a SpreadElement
// in the same function scope (not inside nested functions or try/catch blocks).
func hasVarArgSpreadInScope(node *ast.Node, sym *ast.Symbol, ch *checker.Checker) bool {
	found := false
	var walk func(n *ast.Node, blocked bool)
	walk = func(n *ast.Node, blocked bool) {
		if found {
			return
		}
		switch n.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression,
			ast.KindArrowFunction, ast.KindMethodDeclaration,
			ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
			blocked = true
		case ast.KindClassDeclaration, ast.KindClassExpression:
			blocked = true
		case ast.KindTryStatement, ast.KindCatchClause:
			blocked = true
		}
		if !blocked && n.Kind == ast.KindIdentifier {
			childSym := ch.GetSymbolAtLocation(n)
			if childSym == sym {
				parent := n.Parent
				for parent != nil {
					switch parent.Kind {
					case ast.KindAsExpression, ast.KindTypeAssertionExpression,
						ast.KindNonNullExpression, ast.KindParenthesizedExpression,
						ast.KindSatisfiesExpression:
						parent = parent.Parent
						continue
					}
					break
				}
				if parent != nil && parent.Kind == ast.KindSpreadElement {
					found = true
					return
				}
			}
		}
		n.ForEachChild(func(child *ast.Node) bool {
			walk(child, blocked)
			return found
		})
	}
	walk(node, false)
	return found
}

// isVarArgMutated returns true if the rest param is written to (e.g., args[i] = x)
// anywhere in the subtree, including nested functions. Nested functions capture
// the args table by reference, so mutations there affect the parent scope.
func isVarArgMutated(node *ast.Node, sym *ast.Symbol, ch *checker.Checker) bool {
	mutated := false
	var walk func(n *ast.Node)
	walk = func(n *ast.Node) {
		if mutated {
			return
		}
		// Check for mutations: args[i] = x, args.push(), args.unshift(), etc.
		if n.Kind == ast.KindIdentifier {
			if childSym := ch.GetSymbolAtLocation(n); childSym == sym {
				parent := n.Parent
				if parent == nil {
					// no parent, skip
				} else if parent.Kind == ast.KindElementAccessExpression {
					// args[i] = x — element access as assignment target
					ea := parent.AsElementAccessExpression()
					if ea.Expression == n {
						gp := parent.Parent
						if gp != nil {
							switch gp.Kind {
							case ast.KindBinaryExpression:
								be := gp.AsBinaryExpression()
								op := be.OperatorToken.Kind
								if be.Left == parent && (op == ast.KindEqualsToken || isCompoundAssignment(op)) {
									mutated = true
									return
								}
							case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
								mutated = true
								return
							}
						}
					}
				} else if parent.Kind == ast.KindPropertyAccessExpression {
					// args.push(), args.unshift(), etc. — mutating method calls
					pa := parent.AsPropertyAccessExpression()
					if pa.Expression == n {
						method := pa.Name().AsIdentifier().Text
						switch method {
						case "push", "pop", "shift", "unshift", "splice",
							"sort", "reverse", "fill", "copyWithin":
							mutated = true
							return
						}
					}
				}
			}
		}
		n.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return mutated
		})
	}
	walk(node)
	return mutated
}

// isVarArgSpreadOnly returns true if every reference to sym in the subtree is inside a SpreadElement
// in the same function scope (not inside nested functions or try/catch blocks, which become nested
// functions in Lua via pcall).
func isVarArgSpreadOnly(node *ast.Node, sym *ast.Symbol, ch *checker.Checker) bool {
	result := true
	var walk func(n *ast.Node, blocked bool)
	walk = func(n *ast.Node, blocked bool) {
		if !result {
			return
		}
		// Track entry into scopes that block vararg optimization:
		// - Nested functions: ... doesn't cross function boundaries in Lua
		// - Try/Catch blocks: transpiled to pcall(function() ... end), creating a nested function
		switch n.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression,
			ast.KindArrowFunction, ast.KindMethodDeclaration,
			ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
			blocked = true
		case ast.KindClassDeclaration, ast.KindClassExpression:
			// Property initializers become constructor body — a nested function in Lua
			blocked = true
		case ast.KindTryStatement, ast.KindCatchClause:
			blocked = true
		}
		if n.Kind == ast.KindIdentifier {
			childSym := ch.GetSymbolAtLocation(n)
			if childSym == sym {
				if blocked {
					result = false
					return
				}
				// Walk up through type assertions and parens to find the effective parent.
				// e.g., ...(args as any[]) → SpreadElement > AsExpression > Identifier
				parent := n.Parent
				for parent != nil {
					switch parent.Kind {
					case ast.KindAsExpression, ast.KindTypeAssertionExpression,
						ast.KindNonNullExpression, ast.KindParenthesizedExpression,
						ast.KindSatisfiesExpression:
						parent = parent.Parent
						continue
					}
					break
				}
				if parent == nil || parent.Kind != ast.KindSpreadElement {
					result = false
					return
				}
			}
		}
		// ShorthandPropertyAssignment { b } — GetSymbolAtLocation returns the property symbol,
		// not the value symbol. Check the value symbol separately.
		if n.Kind == ast.KindShorthandPropertyAssignment {
			if valSym := ch.GetShorthandAssignmentValueSymbol(n); valSym == sym {
				result = false
				return
			}
		}
		n.ForEachChild(func(child *ast.Node) bool {
			walk(child, blocked)
			return !result
		})
	}
	// Walk the body node itself (not just children) to handle expression-body arrows
	// where the body IS the expression (e.g., (...args) => args).
	walk(node, false)
	return result
}

// isVarArgSpreadOnly checks if a rest parameter is exclusively used in spread positions
// (no indexing, no other references). Used to decide if table capture can be skipped entirely.
func (t *Transpiler) isVarArgSpreadOnly(param *ast.ParameterDeclaration) bool {

	sym := t.checker.GetSymbolAtLocation(param.Name())
	if sym == nil {
		return false
	}
	// Find the function body. Never skip capture for async functions —
	// ... doesn't cross the async wrapper boundary.
	parent := param.AsNode().Parent
	if parent == nil {
		return false
	}
	if hasAsyncModifier(parent) {
		return false
	}
	var body *ast.Node
	switch parent.Kind {
	case ast.KindFunctionDeclaration:
		body = parent.AsFunctionDeclaration().Body
	case ast.KindFunctionExpression:
		body = parent.AsFunctionExpression().Body
	case ast.KindArrowFunction:
		body = parent.AsArrowFunction().Body
	case ast.KindMethodDeclaration:
		body = parent.AsMethodDeclaration().Body
	case ast.KindConstructor:
		body = parent.AsConstructorDeclaration().Body
	}
	if body == nil {
		return false
	}
	return isVarArgSpreadOnly(body, sym, t.checker)
}

// isOptimizedVarArgSpread checks if an expression being spread is a rest parameter
// that can use ... directly. Triggers for any rest param that has spread usage
// and is not mutated. Only applies when the spread is in the same function scope
// as the rest param declaration — ... doesn't cross function boundaries in Lua.
func (t *Transpiler) isOptimizedVarArgSpread(expr *ast.Node) bool {
	// Unwrap type assertions and parenthesized expressions (e.g., ...(args as any[]))
	inner := skipOuterExpressionsDown(expr)
	if inner.Kind != ast.KindIdentifier || len(t.optimizedVarArgs) == 0 {
		return false
	}
	sym := t.checker.GetSymbolAtLocation(inner)
	if sym == nil {
		return false
	}
	if !t.optimizedVarArgs[t.getOrCreateSymbolID(sym)] {
		return false
	}
	// Verify the spread is in the same function scope as the rest param declaration.
	// ... doesn't cross function boundaries in Lua, so spreads inside nested
	// functions/callbacks must still use unpack(args).
	declFunc := findEnclosingFunction(sym.Declarations[0])
	useFunc := findEnclosingFunction(expr)
	return declFunc == useFunc
}

// findEnclosingFunction walks up the AST to find the nearest enclosing function node.
func findEnclosingFunction(node *ast.Node) *ast.Node {
	for n := node.Parent; n != nil; n = n.Parent {
		switch n.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression,
			ast.KindArrowFunction, ast.KindMethodDeclaration,
			ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
			return n
		}
	}
	return nil
}

// isRestParamReferenced checks if a rest parameter is used anywhere in the function body.
func (t *Transpiler) isRestParamReferenced(param *ast.ParameterDeclaration) bool {

	sym := t.checker.GetSymbolAtLocation(param.Name())
	if sym == nil {
		return true
	}
	// Find the parent function body
	parentFunc := param.AsNode().Parent
	if parentFunc == nil {
		return true
	}
	var body *ast.Node
	switch parentFunc.Kind {
	case ast.KindFunctionDeclaration:
		body = parentFunc.AsFunctionDeclaration().Body
	case ast.KindFunctionExpression:
		body = parentFunc.AsFunctionExpression().Body
	case ast.KindArrowFunction:
		body = parentFunc.AsArrowFunction().Body
	case ast.KindMethodDeclaration:
		body = parentFunc.AsMethodDeclaration().Body
	}
	if body == nil {
		return true
	}
	// Walk the body looking for references to this symbol
	found := false
	var walk func(n *ast.Node)
	walk = func(n *ast.Node) {
		if found {
			return
		}
		if n.Kind == ast.KindIdentifier {
			if childSym := t.checker.GetSymbolAtLocation(n); childSym == sym {
				found = true
				return
			}
		}
		// ShorthandPropertyAssignment { b } — GetSymbolAtLocation returns the property symbol,
		// not the value symbol. Check the value symbol separately.
		if n.Kind == ast.KindShorthandPropertyAssignment {
			if valSym := t.checker.GetShorthandAssignmentValueSymbol(n); valSym == sym {
				found = true
				return
			}
		}
		n.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return found
		})
	}
	// Check the body node itself (for expression-body arrow functions where body IS the identifier)
	walk(body)
	return found
}

// paramName extracts the string name from a parameter's Name() node.
// Goes directly to the TS AST, bypassing transformExpression (avoids export rewriting).
func (t *Transpiler) paramName(nameNode *ast.Node) string {
	if nameNode.Kind == ast.KindIdentifier {
		text := nameNode.AsIdentifier().Text
		if t.hasUnsafeIdentifierName(nameNode) {
			return luaSafeName(text)
		}
		return text
	}
	// Binding pattern — register in destructuredParamNames for preamble generation
	if nameNode.Kind == ast.KindArrayBindingPattern || nameNode.Kind == ast.KindObjectBindingPattern {
		return t.transformBindingPatternParam(nameNode)
	}
	return t.nextTemp("param")
}

// transformParamIdents returns parameter names as lua.Identifier nodes.
func (t *Transpiler) transformParamIdents(params *ast.NodeList, prependSelf bool) (idents []*lua.Identifier, hasRest bool) {
	if prependSelf {
		return t.transformParamIdentsWithContextName(params, "self")
	}
	return t.transformParamIdentsWithContextName(params, "")
}

func (t *Transpiler) transformParamIdentsWithContextName(params *ast.NodeList, contextName string) (idents []*lua.Identifier, hasRest bool) {
	t.bindingPatternCount = 0
	if contextName != "" {
		idents = append(idents, lua.Ident(contextName))
	}
	if params != nil {
		for _, p := range params.Nodes {
			param := p.AsParameterDeclaration()
			nameNode := param.Name()
			name := t.paramName(nameNode)
			if name == "this" {
				continue
			}
			if param.DotDotDotToken != nil {
				hasRest = true
				continue // don't add to param list — will be `...`
			}
			ident := lua.Ident(name)
			// Track renamed parameters for source maps
			if nameNode.Kind == ast.KindIdentifier {
				origName := nameNode.AsIdentifier().Text
				if name != origName {
					t.setNodePosNamed(ident, nameNode, origName)
				}
			}
			idents = append(idents, ident)
		}
	}
	return
}

// transformParamPreamble returns default value guards, rest param capture, and destructuring assignments.
func (t *Transpiler) transformParamPreamble(params *ast.NodeList) []lua.Statement {
	var result []lua.Statement
	// Rest param: local name = {...} — skip if unreferenced or if ONLY used in spread positions
	if params != nil {
		for _, p := range params.Nodes {
			param := p.AsParameterDeclaration()
			if param.DotDotDotToken != nil {
				if !t.isRestParamReferenced(param) {
					continue
				}
				// Skip capture if the param is ONLY used in spread positions —
				// no table needed at all, ... covers everything.
				// If the param is also indexed, we still need the table capture
				// even though spreads will use ... directly.
				if t.isVarArgSpreadOnly(param) {
					continue
				}
				name := t.paramName(param.Name())
				var restExpr lua.Expression
				if t.luaTarget.HasVarargDots() {
					restExpr = lua.Table(lua.Field(lua.Dots()))
				} else {
					restExpr = lua.Ident("arg")
				}
				result = append(result, lua.LocalDecl(
					[]*lua.Identifier{lua.Ident(name)},
					[]lua.Expression{restExpr},
				))
			}
		}
	}
	result = append(result, t.transformDefaultParams(params)...)
	result = append(result, t.transformDestructuredParams(params)...)
	return result
}

// transformDestructuredParams returns unpacking assignments for destructured parameters.
func (t *Transpiler) transformDestructuredParams(params *ast.NodeList) []lua.Statement {
	if params == nil {
		return nil
	}
	var result []lua.Statement
	for _, p := range params.Nodes {
		param := p.AsParameterDeclaration()
		name := param.Name()
		if name.Kind != ast.KindArrayBindingPattern && name.Kind != ast.KindObjectBindingPattern {
			continue
		}
		tempName, ok := t.destructuredParamNames[name]
		if !ok {
			continue
		}
		result = append(result, t.transformBindingPattern(name, lua.Ident(tempName), true, false)...)
	}
	return result
}

// hasParamPreamble returns true if any parameter has a default value, is destructured, or is a rest param.
func (t *Transpiler) hasParamPreamble(params *ast.NodeList) bool {
	if params == nil {
		return false
	}
	for _, p := range params.Nodes {
		param := p.AsParameterDeclaration()
		if param.Initializer != nil || param.DotDotDotToken != nil {
			return true
		}
		name := param.Name()
		if name.Kind == ast.KindArrayBindingPattern || name.Kind == ast.KindObjectBindingPattern {
			return true
		}
	}
	return false
}

// transformDefaultParams returns `if param == nil then param = default end` statements.
func (t *Transpiler) transformDefaultParams(params *ast.NodeList) []lua.Statement {
	if params == nil {
		return nil
	}
	var result []lua.Statement
	for _, p := range params.Nodes {
		param := p.AsParameterDeclaration()
		if param.Initializer == nil {
			continue
		}
		name := t.paramName(param.Name())
		if name == "this" {
			continue
		}
		// Skip default guard when the default value is null/undefined (nil in Lua)
		// because `if x == nil then x = nil end` is a no-op.
		if param.Initializer.Kind == ast.KindNullKeyword ||
			(param.Initializer.Kind == ast.KindIdentifier && param.Initializer.AsIdentifier().Text == "undefined") {
			continue
		}
		defaultVal, prec := t.transformExprInScope(param.Initializer)
		var ifBody []lua.Statement
		ifBody = append(ifBody, prec...)
		// Skip assignment when the transformed default value is nil — assigning nil
		// to a parameter that's already nil is a no-op. Only the preceding statements
		// (side effects from the default expression) matter.
		if !lua.IsNilLiteral(defaultVal) {
			ifBody = append(ifBody, lua.Assign(
				[]lua.Expression{lua.Ident(name)},
				[]lua.Expression{defaultVal},
			))
		}
		if len(ifBody) == 0 {
			continue
		}
		result = append(result, lua.If(
			lua.Binary(lua.Ident(name), lua.OpEq, lua.Nil()),
			&lua.Block{Statements: ifBody},
			nil,
		))
	}
	return result
}

// transformBindingPatternParam handles destructuring patterns used as parameter names.
// Uses TSTL's naming convention: ____bindingPattern0, ____bindingPattern1, etc.
// This is a per-function counter (not the global temp counter) to match TSTL.
func (t *Transpiler) transformBindingPatternParam(node *ast.Node) string {
	name := fmt.Sprintf("____bindingPattern%d", t.bindingPatternCount)
	t.bindingPatternCount++
	if t.destructuredParamNames == nil {
		t.destructuredParamNames = make(map[*ast.Node]string)
	}
	t.destructuredParamNames[node] = name
	return name
}
