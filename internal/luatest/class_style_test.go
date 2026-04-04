package luatest_test

import (
	"testing"

	. "github.com/realcoldfry/tslua/internal/luatest"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

// luabindPreamble loads the mock luabind runtime.
var luabindPreamble = TestdataFile("mock_luabind.lua")

// middleclassPreamble loads the real middleclass library.
var middleclassPreamble = TestdataFile("middleclass.lua") + "\nclass = require and require('middleclass') or class\n"

// For middleclass tests, the preamble needs to make `class` available globally.
func init() {
	// middleclass returns itself from require(); for our inline preamble,
	// the last expression result is already assigned to `class` above via
	// the middleclass module's setmetatable __call.
	middleclassPreamble = "local middleclass = (function()\n" + TestdataFile("middleclass.lua") + "\nend)()\nclass = middleclass\n"
}

// luaTarget picks an available Lua runtime for testing.
const testLuaTarget = transpiler.LuaTargetLua54

func luabindStyle() transpiler.ClassStyle {
	proto := false
	return transpiler.ClassStyle{
		Declare:         transpiler.ClassDeclareCallChain,
		ConstructorName: "__init",
		New:             transpiler.ClassNewDirectCall,
		Super:           transpiler.ClassSuperBaseDirect,
		InstanceOf:      transpiler.ClassInstanceOfNone,
		Prototype:       &proto,
		StaticMembers:   transpiler.ClassStaticError,
	}
}

func middleclassStyle() transpiler.ClassStyle {
	proto := false
	return transpiler.ClassStyle{
		Declare:         transpiler.ClassDeclareCallExtends,
		ConstructorName: "initialize",
		New:             transpiler.ClassNewMethodNew,
		Super:           transpiler.ClassSuperClassSuper,
		InstanceOf:      transpiler.ClassInstanceOfMethod,
		Prototype:       &proto,
	}
}

// ============================================================================
// Luabind tests
// ============================================================================

func TestLuabind_BasicClass(t *testing.T) {
	t.Parallel()
	tsCode := `
class Greeter {
	name: string;
	constructor(name: string) {
		this.name = name;
	}
	greet(): string {
		return "hello " + this.name;
	}
}
export function __main() {
	const g = new Greeter("world");
	return g.greet();
}
`
	opts := Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  luabindStyle(),
		LuaPreamble: luabindPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"hello world"` {
		t.Errorf("got %s, want %q", got, "hello world")
	}
}

func TestLuabind_Inheritance(t *testing.T) {
	t.Parallel()
	tsCode := `
class Animal {
	name: string;
	constructor(name: string) {
		this.name = name;
	}
	speak(): string {
		return this.name + " makes a sound";
	}
}
class Dog extends Animal {
	breed: string;
	constructor(name: string, breed: string) {
		super(name);
		this.breed = breed;
	}
	speak(): string {
		return this.name + " barks";
	}
}
export function __main() {
	const d = new Dog("Rex", "Husky");
	return d.speak() + " " + d.breed;
}
`
	opts := Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  luabindStyle(),
		LuaPreamble: luabindPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"Rex barks Husky"` {
		t.Errorf("got %s, want %q", got, "Rex barks Husky")
	}
}

func TestLuabind_SuperMethod(t *testing.T) {
	t.Parallel()
	tsCode := `
class Base {
	value(): string {
		return "base";
	}
}
class Child extends Base {
	value(): string {
		return super.value() + "+child";
	}
}
export function __main() {
	const c = new Child();
	return c.value();
}
`
	opts := Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  luabindStyle(),
		LuaPreamble: luabindPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"base+child"` {
		t.Errorf("got %s, want %q", got, "base+child")
	}
}

// ============================================================================
// Middleclass tests
// ============================================================================

func TestMiddleclass_BasicClass(t *testing.T) {
	t.Parallel()
	tsCode := `
class Greeter {
	name: string;
	constructor(name: string) {
		this.name = name;
	}
	greet(): string {
		return "hello " + this.name;
	}
}
export function __main() {
	const g = new Greeter("world");
	return g.greet();
}
`
	opts := Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  middleclassStyle(),
		LuaPreamble: middleclassPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"hello world"` {
		t.Errorf("got %s, want %q", got, "hello world")
	}
}

func TestMiddleclass_Inheritance(t *testing.T) {
	t.Parallel()
	tsCode := `
class Animal {
	name: string;
	constructor(name: string) {
		this.name = name;
	}
	speak(): string {
		return this.name + " makes a sound";
	}
}
class Dog extends Animal {
	breed: string;
	constructor(name: string, breed: string) {
		super(name);
		this.breed = breed;
	}
	speak(): string {
		return this.name + " barks";
	}
}
export function __main() {
	const d = new Dog("Rex", "Husky");
	return d.speak() + " " + d.breed;
}
`
	opts := Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  middleclassStyle(),
		LuaPreamble: middleclassPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"Rex barks Husky"` {
		t.Errorf("got %s, want %q", got, "Rex barks Husky")
	}
}

func TestMiddleclass_SuperMethod(t *testing.T) {
	t.Parallel()
	tsCode := `
class Base {
	value(): string {
		return "base";
	}
}
class Child extends Base {
	value(): string {
		return super.value() + "+child";
	}
}
export function __main() {
	const c = new Child();
	return c.value();
}
`
	opts := Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  middleclassStyle(),
		LuaPreamble: middleclassPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"base+child"` {
		t.Errorf("got %s, want %q", got, "base+child")
	}
}

func TestMiddleclass_InstanceOf(t *testing.T) {
	t.Parallel()
	tsCode := `
class Animal {}
class Dog extends Animal {}
export function __main() {
	const d = new Dog();
	const results: boolean[] = [];
	results.push(d instanceof Dog);
	results.push(d instanceof Animal);
	return results.join(",");
}
`
	opts := Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  middleclassStyle(),
		LuaPreamble: middleclassPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"true,true"` {
		t.Errorf("got %s, want %q", got, "true,true")
	}
}
