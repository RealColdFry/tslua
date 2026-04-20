package luatest_test

import (
	"strings"
	"testing"

	"github.com/realcoldfry/tslua/internal/lualib"
	. "github.com/realcoldfry/tslua/internal/luatest"
	"github.com/realcoldfry/tslua/internal/transpiler"
)

// luabindPreamble loads the mock luabind runtime.
var luabindPreamble = TestdataFile("mock_luabind.lua")

// middleclassPreamble registers the real kikito/middleclass library under
// `package.loaded["middleclass"]` so the transpiler-injected
// `local class = require("middleclass")` header resolves. The module source
// comes from the same embedded copy shipped by `internal/lualib/middleclass/`.
var middleclassPreamble = "package.loaded[\"middleclass\"] = (function()\n" +
	string(lualib.Middleclass()) +
	"\nend)()\n"

// luaTarget picks an available Lua runtime for testing.
const testLuaTarget = transpiler.LuaTargetLua54

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
		ClassStyle:  transpiler.ClassStyleLuabind,
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
		ClassStyle:  transpiler.ClassStyleLuabind,
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
		ClassStyle:  transpiler.ClassStyleLuabind,
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
		ClassStyle:  transpiler.ClassStyleMiddleclass,
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
		ClassStyle:  transpiler.ClassStyleMiddleclass,
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
		ClassStyle:  transpiler.ClassStyleMiddleclass,
		LuaPreamble: middleclassPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"base+child"` {
		t.Errorf("got %s, want %q", got, "base+child")
	}
}

// TestMiddleclass_RequireHeader verifies the transpiler auto-injects
// `local class = require("middleclass")` only when middleclass class-style
// actually emits a class — and that TSTL-style output stays free of the
// require.
func TestMiddleclass_RequireHeader(t *testing.T) {
	t.Parallel()
	const requireLine = `local class = require("middleclass")`
	tsWithClass := `class Foo {} export const x = new Foo();`
	tsNoClass := `export const x = 1 + 2;`

	// Middleclass style + class → header present, UsesMiddleclass true.
	results := TranspileTS(t, tsWithClass, Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  transpiler.ClassStyleMiddleclass,
		LuaPreamble: middleclassPreamble,
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].UsesMiddleclass {
		t.Errorf("UsesMiddleclass = false, want true for middleclass style with class")
	}
	if !strings.Contains(results[0].Lua, requireLine) {
		t.Errorf("middleclass output missing %q header:\n%s", requireLine, results[0].Lua)
	}

	// Middleclass style but no class → no header, no flag.
	results = TranspileTS(t, tsNoClass, Opts{
		LuaTarget:   testLuaTarget,
		ClassStyle:  transpiler.ClassStyleMiddleclass,
		LuaPreamble: middleclassPreamble,
	})
	if results[0].UsesMiddleclass {
		t.Errorf("UsesMiddleclass = true, want false when no class emitted")
	}
	if strings.Contains(results[0].Lua, requireLine) {
		t.Errorf("middleclass-style output has require header when no class emitted:\n%s", results[0].Lua)
	}

	// TSTL style with class → no middleclass header.
	results = TranspileTS(t, tsWithClass, Opts{
		LuaTarget: testLuaTarget,
	})
	if results[0].UsesMiddleclass {
		t.Errorf("UsesMiddleclass = true, want false for TSTL style")
	}
	if strings.Contains(results[0].Lua, requireLine) {
		t.Errorf("TSTL-style output has middleclass require header:\n%s", results[0].Lua)
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
		ClassStyle:  transpiler.ClassStyleMiddleclass,
		LuaPreamble: middleclassPreamble,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"true,true"` {
		t.Errorf("got %s, want %q", got, "true,true")
	}
}

// ============================================================================
// Inline tests (rbxts-style, no runtime library)
// ============================================================================

func TestInline_BasicClass(t *testing.T) {
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
		LuaTarget:  testLuaTarget,
		ClassStyle: transpiler.ClassStyleInline,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"hello world"` {
		t.Errorf("got %s, want %q", got, "hello world")
	}
}

func TestInline_Inheritance(t *testing.T) {
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
		LuaTarget:  testLuaTarget,
		ClassStyle: transpiler.ClassStyleInline,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"Rex barks Husky"` {
		t.Errorf("got %s, want %q", got, "Rex barks Husky")
	}
}

func TestInline_SuperMethod(t *testing.T) {
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
		LuaTarget:  testLuaTarget,
		ClassStyle: transpiler.ClassStyleInline,
	}
	results := TranspileTS(t, tsCode, opts)
	got := RunLua(t, results, "mod.__main()", opts)
	if got != `"base+child"` {
		t.Errorf("got %s, want %q", got, "base+child")
	}
}
