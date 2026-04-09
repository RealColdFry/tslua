package luatest

import (
	"strings"
	"testing"
)

func TestModuleExportAsDefault(t *testing.T) {
	t.Parallel()
	// Tests moduleExportName with KindDefaultKeyword
	code := `
		const value = 42;
		export { value as default };
	`
	results := TranspileTS(t, "export function __main() { return 0; }\n"+code, Opts{})
	for _, r := range results {
		if strings.HasSuffix(r.FileName, "main.ts") {
			if !strings.Contains(r.Lua, "default") {
				t.Errorf("expected 'default' export in output, got:\n%s", r.Lua)
			}
		}
	}
}

func TestNestedNamespace(t *testing.T) {
	t.Parallel()
	// Tests createModuleLocalName with nested dotted namespaces
	ExpectFunction(t, `
		namespace Foo {
			export namespace Bar {
				export function getValue() { return 42; }
			}
		}
		return Foo.Bar.getValue();
	`, `42`, Opts{})
}

func TestNamespaceExportedValue(t *testing.T) {
	t.Parallel()
	ExpectFunction(t, `
		namespace Config {
			export const x = 10;
			export const y = 20;
		}
		return Config.x + Config.y;
	`, `30`, Opts{})
}
