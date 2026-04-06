// Tests that type-only imports and unused value imports are elided from Lua output.
package luatest

import (
	"strings"
	"testing"
)

const moduleDeclaration = `declare module "module" {
	export type Type = string;
	export declare const value: string;
}`

func TestModuleElision_NamedTypeImport(t *testing.T) {
	t.Parallel()
	tsCode := moduleDeclaration + "\n" + `import { Type } from "module";
const foo: Type = "bar";`
	results := TranspileTS(t, tsCode, Opts{ExtraFiles: map[string]string{"module.d.ts": moduleDeclaration}})
	lua := mainLua(t, results)
	if strings.Contains(lua, `require("module")`) {
		t.Errorf("type-only import should be elided, got:\n%s", lua)
	}
}

func TestModuleElision_ValueImportUsedOnlyAsType(t *testing.T) {
	t.Parallel()
	tsCode := moduleDeclaration + "\n" + `import { value } from "module";
const foo: typeof value = "bar";`
	results := TranspileTS(t, tsCode, Opts{ExtraFiles: map[string]string{"module.d.ts": moduleDeclaration}})
	lua := mainLua(t, results)
	if strings.Contains(lua, `require("module")`) {
		t.Errorf("value import used only as type should be elided, got:\n%s", lua)
	}
}

func TestModuleElision_NamespaceImportUnusedValues(t *testing.T) {
	t.Parallel()
	tsCode := moduleDeclaration + "\n" + `import * as module from "module";
const foo: module.Type = "bar";`
	results := TranspileTS(t, tsCode, Opts{ExtraFiles: map[string]string{"module.d.ts": moduleDeclaration}})
	lua := mainLua(t, results)
	if strings.Contains(lua, `require("module")`) {
		t.Errorf("namespace import with unused values should be elided, got:\n%s", lua)
	}
}

func TestModuleElision_ImportEqualsDeclaration(t *testing.T) {
	t.Parallel()
	tsCode := moduleDeclaration + "\n" + `import module = require("module");
const foo: module.Type = "bar";`
	results := TranspileTS(t, tsCode, Opts{ExtraFiles: map[string]string{"module.d.ts": moduleDeclaration}})
	lua := mainLua(t, results)
	if strings.Contains(lua, `require("module")`) {
		t.Errorf("import = declaration used only as type should be elided, got:\n%s", lua)
	}
}
