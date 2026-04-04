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
