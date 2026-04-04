package transpiler

import "github.com/realcoldfry/tslua/internal/lua"

// ClassStyle controls how TypeScript classes are emitted to Lua.
// The zero value produces TSTL-compatible output (prototype chains via __TS__Class).
type ClassStyle struct {
	// Declare controls how class declarations are emitted.
	//   "tstl"         → __TS__Class() + __TS__ClassExtends(cls, base)  [default]
	//   "call-chain"   → class("Name")(base)                           [luabind]
	//   "call-extends" → class("Name", base)                           [middleclass]
	Declare ClassDeclareStyle `json:"declare,omitempty"`

	// ConstructorName is the method name used for constructors.
	//   "____constructor" [default/TSTL], "__init" [luabind], "initialize" [middleclass]
	ConstructorName string `json:"constructorName,omitempty"`

	// New controls how `new Class(args)` is emitted.
	//   "tstl-new"     → __TS__New(Class, args)   [default]
	//   "direct-call"  → Class(args)              [luabind]
	//   "method-new"   → Class:new(args)          [middleclass]
	New ClassNewStyle `json:"new,omitempty"`

	// Super controls how super constructor and method calls are emitted.
	//   "tstl"         → Base.prototype.____constructor(self, args)  [default]
	//   "base-direct"  → Base.__init(self, args)                    [luabind]
	//   "class-super"  → Class.super.initialize(self, args)         [middleclass]
	Super ClassSuperStyle `json:"super,omitempty"`

	// InstanceOf controls how `instanceof` checks are emitted.
	//   "tstl"    → __TS__InstanceOf(obj, Class)  [default]
	//   "method"  → obj:isInstanceOf(Class)       [middleclass]
	//   "none"    → emit diagnostic error         [luabind — no runtime support]
	InstanceOf ClassInstanceOfStyle `json:"instanceOf,omitempty"`

	// Prototype controls whether methods are attached to Class.prototype or directly to Class.
	//   true  → Class.prototype.method = fn  [default/TSTL]
	//   false → Class.method = fn            [luabind, middleclass]
	Prototype *bool `json:"prototype,omitempty"`

	// StaticMembers controls whether static members are allowed.
	//   "allow" → emit normally     [default/TSTL, middleclass]
	//   "error" → emit diagnostic   [luabind — C++ binding doesn't support statics]
	StaticMembers ClassStaticPolicy `json:"staticMembers,omitempty"`
}

// ClassDeclareStyle enumerates class declaration emit patterns.
type ClassDeclareStyle string

const (
	ClassDeclareTSTL         ClassDeclareStyle = ""             // default: __TS__Class()
	ClassDeclareTSTLExplicit ClassDeclareStyle = "tstl"         // explicit default
	ClassDeclareCallChain    ClassDeclareStyle = "call-chain"   // class("Name")(base)
	ClassDeclareCallExtends  ClassDeclareStyle = "call-extends" // class("Name", base)
)

// ClassNewStyle enumerates `new` expression emit patterns.
type ClassNewStyle string

const (
	ClassNewTSTL         ClassNewStyle = ""            // default: __TS__New(Class, args)
	ClassNewTSTLExplicit ClassNewStyle = "tstl-new"    // explicit default
	ClassNewDirectCall   ClassNewStyle = "direct-call" // Class(args)
	ClassNewMethodNew    ClassNewStyle = "method-new"  // Class:new(args)
)

// ClassSuperStyle enumerates super call emit patterns.
type ClassSuperStyle string

const (
	ClassSuperTSTL         ClassSuperStyle = ""            // default: Base.prototype.____constructor(self)
	ClassSuperTSTLExplicit ClassSuperStyle = "tstl"        // explicit default
	ClassSuperBaseDirect   ClassSuperStyle = "base-direct" // Base.__init(self)
	ClassSuperClassSuper   ClassSuperStyle = "class-super" // Class.super.initialize(self)
)

// ClassInstanceOfStyle enumerates instanceof emit patterns.
type ClassInstanceOfStyle string

const (
	ClassInstanceOfTSTL         ClassInstanceOfStyle = ""       // default: __TS__InstanceOf(obj, Class)
	ClassInstanceOfTSTLExplicit ClassInstanceOfStyle = "tstl"   // explicit default
	ClassInstanceOfMethod       ClassInstanceOfStyle = "method" // obj:isInstanceOf(Class)
	ClassInstanceOfNone         ClassInstanceOfStyle = "none"   // emit diagnostic error
)

// ClassStaticPolicy enumerates static member handling.
type ClassStaticPolicy string

const (
	ClassStaticAllow         ClassStaticPolicy = ""      // default: emit normally
	ClassStaticAllowExplicit ClassStaticPolicy = "allow" // explicit default
	ClassStaticError         ClassStaticPolicy = "error" // emit diagnostic error
)

// constructorName returns the effective constructor method name.
func (cs *ClassStyle) constructorName() string {
	if cs.ConstructorName != "" {
		return cs.ConstructorName
	}
	return "____constructor"
}

// usesPrototype returns whether methods go on .prototype (true) or directly on the class (false).
func (cs *ClassStyle) usesPrototype() bool {
	if cs.Prototype != nil {
		return *cs.Prototype
	}
	return true // TSTL default
}

// isTSTL returns true if this class style produces TSTL-compatible output.
func (cs *ClassStyle) isTSTL() bool {
	return (cs.Declare == ClassDeclareTSTL || cs.Declare == ClassDeclareTSTLExplicit) &&
		cs.ConstructorName == "" &&
		(cs.New == ClassNewTSTL || cs.New == ClassNewTSTLExplicit) &&
		(cs.Super == ClassSuperTSTL || cs.Super == ClassSuperTSTLExplicit) &&
		(cs.InstanceOf == ClassInstanceOfTSTL || cs.InstanceOf == ClassInstanceOfTSTLExplicit) &&
		(cs.Prototype == nil || *cs.Prototype) &&
		(cs.StaticMembers == ClassStaticAllow || cs.StaticMembers == ClassStaticAllowExplicit)
}

// classInitExpr returns the expression to create a new class.
// For TSTL: __TS__Class()
// For call-chain: class("Name")()           [no base]
//
//	class("Name")(baseExpr)    [with base]
//
// For call-extends: class("Name")            [no base]
//
//	class("Name", baseExpr)  [with base]
func (cs *ClassStyle) classInitExpr(className string, baseExpr lua.Expression) lua.Expression {
	switch cs.Declare {
	case ClassDeclareCallChain:
		nameCall := lua.Call(lua.Ident("class"), lua.Str(className))
		if baseExpr != nil {
			return lua.Call(nameCall, baseExpr)
		}
		return lua.Call(nameCall)
	case ClassDeclareCallExtends:
		if baseExpr != nil {
			return lua.Call(lua.Ident("class"), lua.Str(className), baseExpr)
		}
		return lua.Call(lua.Ident("class"), lua.Str(className))
	default:
		return nil // caller handles TSTL path
	}
}

// methodTarget returns the expression to attach a method to.
// For TSTL (prototype=true): classRef.prototype
// For non-prototype: classRef directly
func (cs *ClassStyle) methodTarget(classRef lua.Expression) lua.Expression {
	if cs.usesPrototype() {
		return lua.Index(classRef, lua.Str("prototype"))
	}
	return classRef
}

// constructorAccess returns the expression for the constructor method.
// e.g., classRef.prototype.____constructor or classRef.__init
func (cs *ClassStyle) constructorAccess(classRef lua.Expression) lua.Expression {
	target := cs.methodTarget(classRef)
	return lua.Index(target, lua.Str(cs.constructorName()))
}

// newExpr returns the expression for `new Class(args)`.
// For TSTL: __TS__New(classExpr, args...)
// For direct-call: classExpr(args...)
// For method-new: classExpr:new(args...)
func (cs *ClassStyle) newExpr(classExpr lua.Expression, args []lua.Expression) lua.Expression {
	switch cs.New {
	case ClassNewDirectCall:
		return lua.Call(classExpr, args...)
	case ClassNewMethodNew:
		return lua.MethodCall(classExpr, "new", args...)
	default:
		return nil // caller handles TSTL path
	}
}

// superConstructorCall returns the expression for super(args).
// For TSTL: Base.prototype.____constructor(self, args...)
// For base-direct: Base.__init(self, args...)
// For class-super: classRef.super.initialize(self, args...)
func (cs *ClassStyle) superConstructorCall(baseExpr lua.Expression, classRef lua.Expression, args []lua.Expression) lua.Expression {
	selfArgs := append([]lua.Expression{lua.Ident("self")}, args...)
	switch cs.Super {
	case ClassSuperBaseDirect:
		fn := lua.Index(baseExpr, lua.Str(cs.constructorName()))
		return lua.Call(fn, selfArgs...)
	case ClassSuperClassSuper:
		fn := memberAccess(classRef, "super", cs.constructorName())
		return lua.Call(fn, selfArgs...)
	default:
		return nil // caller handles TSTL path
	}
}

// superMethodCall returns the expression for super.method(args).
// For TSTL: Base.prototype.method(self, args...)
// For base-direct: Base.method(self, args...)
// For class-super: classRef.super.method(self, args...)
func (cs *ClassStyle) superMethodCall(baseExpr lua.Expression, classRef lua.Expression, method string, isStatic bool, args []lua.Expression) lua.Expression {
	selfArgs := append([]lua.Expression{lua.Ident("self")}, args...)
	switch cs.Super {
	case ClassSuperBaseDirect:
		fn := lua.Index(baseExpr, lua.Str(method))
		return lua.Call(fn, selfArgs...)
	case ClassSuperClassSuper:
		fn := memberAccess(classRef, "super", method)
		return lua.Call(fn, selfArgs...)
	default:
		return nil // caller handles TSTL path
	}
}
