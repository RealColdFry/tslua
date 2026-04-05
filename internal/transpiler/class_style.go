package transpiler

import "github.com/realcoldfry/tslua/internal/lua"

// ClassStyle controls how TypeScript classes are emitted to Lua.
// Use a preset name: "" or "tstl" (default), "luabind", "middleclass", "inline".
type ClassStyle string

const (
	ClassStyleTSTL        ClassStyle = ""            // default: TSTL prototype chains
	ClassStyleLuabind     ClassStyle = "luabind"     // luabind (C++ binding)
	ClassStyleMiddleclass ClassStyle = "middleclass" // middleclass library
	ClassStyleInline      ClassStyle = "inline"      // inline setmetatable (rbxts-style, no runtime library)
)

func (cs ClassStyle) isTSTL() bool {
	return cs == ClassStyleTSTL || cs == "tstl"
}

func (cs ClassStyle) isInline() bool {
	return cs == ClassStyleInline
}

func (cs ClassStyle) constructorName() string {
	switch cs {
	case ClassStyleLuabind:
		return "__init"
	case ClassStyleMiddleclass:
		return "initialize"
	case ClassStyleInline:
		return "constructor"
	default:
		return "____constructor"
	}
}

func (cs ClassStyle) usesPrototype() bool {
	return cs.isTSTL()
}

// classInitExpr returns the expression to create a new class (for library-based styles).
// Returns nil for TSTL and inline styles, which handle initialization differently.
func (cs ClassStyle) classInitExpr(className string, baseExpr lua.Expression) lua.Expression {
	switch cs {
	case ClassStyleLuabind:
		nameCall := lua.Call(lua.Ident("class"), lua.Str(className))
		if baseExpr != nil {
			return lua.Call(nameCall, baseExpr)
		}
		return lua.Call(nameCall)
	case ClassStyleMiddleclass:
		if baseExpr != nil {
			return lua.Call(lua.Ident("class"), lua.Str(className), baseExpr)
		}
		return lua.Call(lua.Ident("class"), lua.Str(className))
	default:
		return nil
	}
}

// inlineInitStatements generates the inline class initialization boilerplate:
//
//	[local super = baseExpr]
//	ClassName = setmetatable({}, { __tostring = function() return "Name" end [, __index = super] })
//	ClassName.__index = ClassName
//	function ClassName.new(...)
//	    local self = setmetatable({}, ClassName)
//	    return self:constructor(...) or self
//	end
func (cs ClassStyle) inlineInitStatements(classRef lua.Expression, origName string, baseExpr lua.Expression) []lua.Statement {
	var stmts []lua.Statement

	// local super = baseExpr (if extends)
	if baseExpr != nil {
		stmts = append(stmts, lua.LocalDecl(
			[]*lua.Identifier{lua.Ident("super")},
			[]lua.Expression{baseExpr},
		))
	}

	// Build metatable: { __tostring = function() return "Name" end [, __index = super] }
	tostringFn := &lua.FunctionExpression{
		Body: &lua.Block{Statements: []lua.Statement{
			lua.Return(lua.Str(origName)),
		}},
	}
	metatableFields := []*lua.TableFieldExpression{
		lua.KeyField(lua.Str("__tostring"), tostringFn),
	}
	if baseExpr != nil {
		metatableFields = append(metatableFields,
			lua.KeyField(lua.Str("__index"), lua.Ident("super")),
		)
	}
	metatable := lua.Table(metatableFields...)

	// ClassName = setmetatable({}, metatable)
	stmts = append(stmts, lua.Assign(
		[]lua.Expression{classRef},
		[]lua.Expression{lua.Call(lua.Ident("setmetatable"), lua.Table(), metatable)},
	))

	// ClassName.__index = ClassName
	stmts = append(stmts, lua.Assign(
		[]lua.Expression{lua.Index(classRef, lua.Str("__index"))},
		[]lua.Expression{classRef},
	))

	// function ClassName.new(...)
	//     local self = setmetatable({}, ClassName)
	//     return self:constructor(...) or self
	// end
	newFn := &lua.FunctionExpression{
		Dots:  true,
		Flags: lua.FlagDeclaration,
		Body: &lua.Block{Statements: []lua.Statement{
			lua.LocalDecl(
				[]*lua.Identifier{lua.Ident("self")},
				[]lua.Expression{lua.Call(lua.Ident("setmetatable"), lua.Table(), classRef)},
			),
			lua.Return(lua.Binary(
				lua.MethodCall(lua.Ident("self"), cs.constructorName(), lua.Dots()),
				lua.OpOr,
				lua.Ident("self"),
			)),
		}},
	}
	stmts = append(stmts, lua.Assign(
		[]lua.Expression{lua.Index(classRef, lua.Str("new"))},
		[]lua.Expression{newFn},
	))

	return stmts
}

// methodTarget returns the expression to attach a method to.
func (cs ClassStyle) methodTarget(classRef lua.Expression) lua.Expression {
	if cs.usesPrototype() {
		return lua.Index(classRef, lua.Str("prototype"))
	}
	return classRef
}

// constructorAccess returns the expression for the constructor method.
func (cs ClassStyle) constructorAccess(classRef lua.Expression) lua.Expression {
	target := cs.methodTarget(classRef)
	return lua.Index(target, lua.Str(cs.constructorName()))
}

// newExpr returns the expression for `new Class(args)`.
// Returns nil for TSTL style (caller handles via __TS__New).
func (cs ClassStyle) newExpr(classExpr lua.Expression, args []lua.Expression) lua.Expression {
	switch cs {
	case ClassStyleLuabind:
		return lua.Call(classExpr, args...)
	case ClassStyleMiddleclass:
		return lua.MethodCall(classExpr, "new", args...)
	case ClassStyleInline:
		return lua.Call(lua.Index(classExpr, lua.Str("new")), args...)
	default:
		return nil
	}
}

// superConstructorCall returns the expression for super(args).
// Returns nil for TSTL style (caller handles via Base.prototype.____constructor).
func (cs ClassStyle) superConstructorCall(baseExpr lua.Expression, classRef lua.Expression, args []lua.Expression) lua.Expression {
	selfArgs := append([]lua.Expression{lua.Ident("self")}, args...)
	switch cs {
	case ClassStyleLuabind, ClassStyleInline:
		fn := lua.Index(baseExpr, lua.Str(cs.constructorName()))
		return lua.Call(fn, selfArgs...)
	case ClassStyleMiddleclass:
		fn := memberAccess(classRef, "super", cs.constructorName())
		return lua.Call(fn, selfArgs...)
	default:
		return nil
	}
}

// superMethodCall returns the expression for super.method(args).
// Returns nil for TSTL style (caller handles via Base.prototype.method).
func (cs ClassStyle) superMethodCall(baseExpr lua.Expression, classRef lua.Expression, method string, isStatic bool, args []lua.Expression) lua.Expression {
	selfArgs := append([]lua.Expression{lua.Ident("self")}, args...)
	switch cs {
	case ClassStyleLuabind, ClassStyleInline:
		fn := lua.Index(baseExpr, lua.Str(method))
		return lua.Call(fn, selfArgs...)
	case ClassStyleMiddleclass:
		fn := memberAccess(classRef, "super", method)
		return lua.Call(fn, selfArgs...)
	default:
		return nil
	}
}

// instanceOfBehavior returns how instanceof checks should be emitted.
func (cs ClassStyle) instanceOfBehavior() string {
	switch cs {
	case ClassStyleMiddleclass:
		return "method"
	case ClassStyleLuabind, ClassStyleInline:
		return "none"
	default:
		return "tstl"
	}
}
