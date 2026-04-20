// Tests for @customName annotation that renames symbols in Lua output.
package luatest

import (
	"strings"
	"testing"
)

func TestCustomName_RenameFunction(t *testing.T) {
	t.Parallel()
	tsCode := `/** @customName test2 **/
function test(this: void): number { return 3; }
export const result: number = test();`
	results := TranspileTS(t, tsCode, Opts{})
	got := RunLua(t, results, `mod["result"]`, Opts{})
	if got != "3" {
		t.Errorf("got %s, want 3", got)
	}
	lua := mainLua(t, results)
	if !strings.Contains(lua, "function test2(") {
		t.Errorf("expected declaration to use customName 'test2', got:\n%s", lua)
	}
}

func TestCustomName_RenameVariable(t *testing.T) {
	t.Parallel()
	tsCode := `/** @customName result2 **/
export const result: number = 3;`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "result2 =") {
		t.Errorf("expected variable to use customName 'result2', got:\n%s", lua)
	}
	if strings.Contains(lua, "result =") && !strings.Contains(lua, "result2 =") {
		t.Errorf("expected original name 'result' to be replaced, got:\n%s", lua)
	}
}

func TestCustomName_RenameVariableSubsequentAssignment(t *testing.T) {
	t.Parallel()
	tsCode := `/** @customName b */
let a = 0;
a = 1;
export const result = a;`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if strings.Contains(lua, "\na = ") || strings.Contains(lua, " a = ") {
		t.Errorf("expected subsequent assignment to use customName 'b', got:\n%s", lua)
	}
	got := RunLua(t, results, `mod["result"]`, Opts{})
	if got != "1" {
		t.Errorf("got %s, want 1", got)
	}
}

func TestCustomName_RenameVariableMultiDecl(t *testing.T) {
	t.Parallel()
	// @customName on a multi-declaration statement applies only to the first
	// declaration, matching TSTL. `c` must keep its original name.
	tsCode := `/** @customName b */
let a = 0, c = 1;
a = 2;
c = 3;
export const result = a + c;`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "local b = 0") {
		t.Errorf("expected first decl to use customName 'b', got:\n%s", lua)
	}
	if !strings.Contains(lua, "local c = 1") {
		t.Errorf("expected second decl to keep name 'c', got:\n%s", lua)
	}
	if strings.Contains(lua, "local b = 1") {
		t.Errorf("customName leaked to second declaration, got:\n%s", lua)
	}
	got := RunLua(t, results, `mod["result"]`, Opts{})
	if got != "5" {
		t.Errorf("got %s, want 5", got)
	}
}

func TestCustomName_RenameVariableClosureCapture(t *testing.T) {
	t.Parallel()
	tsCode := `/** @customName b */
let a = 10;
function f() { return a; }
export const result = f();`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "local b = 10") {
		t.Errorf("expected decl to use customName 'b', got:\n%s", lua)
	}
	if !strings.Contains(lua, "return b") {
		t.Errorf("expected captured reference to use customName 'b', got:\n%s", lua)
	}
	got := RunLua(t, results, `mod["result"]`, Opts{})
	if got != "10" {
		t.Errorf("got %s, want 10", got)
	}
}

func TestCustomName_RenameVariableCompoundAssignment(t *testing.T) {
	t.Parallel()
	tsCode := `/** @customName b */
let a = 0;
a += 5;
a++;
export const result = a;`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if strings.Contains(lua, "\na = ") || strings.Contains(lua, " a = ") || strings.Contains(lua, "a + 1") && !strings.Contains(lua, "b + 1") {
		t.Errorf("expected compound/update to use customName 'b', got:\n%s", lua)
	}
	got := RunLua(t, results, `mod["result"]`, Opts{})
	if got != "6" {
		t.Errorf("got %s, want 6", got)
	}
}

func TestCustomName_RenameVariableShadowedInner(t *testing.T) {
	t.Parallel()
	// Inner-scope redeclaration must NOT inherit the outer's customName.
	tsCode := `/** @customName b */
let a = 0;
{
  let a = 99;
  a = 100;
}
a = 2;
export const result = a;`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "local b = 0") {
		t.Errorf("expected outer decl to use customName 'b', got:\n%s", lua)
	}
	if !strings.Contains(lua, "local a = 99") {
		t.Errorf("expected inner decl to keep name 'a', got:\n%s", lua)
	}
	if !strings.Contains(lua, "b = 2") {
		t.Errorf("expected outer assignment to use customName 'b', got:\n%s", lua)
	}
	got := RunLua(t, results, `mod["result"]`, Opts{})
	if got != "2" {
		t.Errorf("got %s, want 2", got)
	}
}

func TestCustomName_RenameClass(t *testing.T) {
	t.Parallel()
	tsCode := `/** @customName Class2 **/
class Class {
	test: string;
	constructor(test: string) { this.test = test; }
}
export const result = new Class("hello world");`
	results := TranspileTS(t, tsCode, Opts{})
	got := RunLua(t, results, `mod["result"]["test"]`, Opts{})
	if got != `"hello world"` {
		t.Errorf("got %s, want \"hello world\"", got)
	}
}

func TestCustomName_RenameDeclaredFunction(t *testing.T) {
	t.Parallel()
	tsCode := `/** @customName customAdd */
declare function add(a: number, b: number): number;
export const result = add(1, 2);`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "customAdd(") {
		t.Errorf("expected reference to use customName 'customAdd', got:\n%s", lua)
	}
}

func TestCustomName_RenameNamespace(t *testing.T) {
	t.Parallel()
	tsCode := `/** @customName NS2 */
namespace NS {
	export const value = 42;
}
export const result = NS.value;`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "NS2") {
		t.Errorf("expected namespace to use customName 'NS2', got:\n%s", lua)
	}
}

func TestCustomName_DeclareNamespacePropertyAccess(t *testing.T) {
	t.Parallel()
	tsCode := `/** @noSelf */
declare namespace TestNamespace {
	/** @customName pass */
	function fail(): void;
}
TestNamespace.fail();`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "TestNamespace.pass()") {
		t.Errorf("expected property access to use customName 'pass', got:\n%s", lua)
	}
	if strings.Contains(lua, "TestNamespace.fail()") {
		t.Errorf("original name 'fail' should not appear, got:\n%s", lua)
	}
}

func TestCustomName_DeclareNamespaceWithExtraComment(t *testing.T) {
	t.Parallel()
	tsCode := `/** @noSelf */
declare namespace TestNamespace {
	/**
	 * @customName pass
	 * The first word should not be included.
	 **/
	function fail(): void;
}
TestNamespace.fail();`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "TestNamespace.pass()") {
		t.Errorf("expected property access to use customName 'pass' (first word only), got:\n%s", lua)
	}
}

func TestCustomName_NamespaceFunctionCall(t *testing.T) {
	t.Parallel()
	tsCode := `namespace A {
	/** @customName Func2 */
	export function Func(): string { return "hi"; }
}
export const result = A.Func();`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "A:Func2()") && !strings.Contains(lua, "A.Func2(") {
		t.Errorf("expected call to use customName 'Func2', got:\n%s", lua)
	}
}

func TestCustomName_NamespaceFunctionDeclaration(t *testing.T) {
	t.Parallel()
	tsCode := `namespace A {
	/** @customName Func2 */
	export function Func(): string { return "hi"; }
}`
	results := TranspileTS(t, tsCode, Opts{})
	lua := mainLua(t, results)
	if !strings.Contains(lua, "A.Func2(") {
		t.Errorf("expected declaration to use customName 'Func2', got:\n%s", lua)
	}
}
