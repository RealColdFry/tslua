package luatest

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/shim/ast"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
)

func TestOptionalChainBuiltin_NoDiagnosticForNonOptionalCall(t *testing.T) {
	// When a builtin method call (.has, .push, etc.) is part of an optional chain
	// but the call itself is NOT optional (no ?. on the call), there should be
	// no TL100039 diagnostic. The optional access is on the object, not the call.
	t.Parallel()

	diagCode := func(d *ast.Diagnostic) dw.DiagCode {
		return dw.DiagCode(ast.Diagnostic_Code(d))
	}

	hasDiag := func(diags []*ast.Diagnostic, code dw.DiagCode) bool {
		for _, d := range diags {
			if diagCode(d) == code {
				return true
			}
		}
		return false
	}

	diagMessages := func(diags []*ast.Diagnostic) string {
		var msgs []string
		for _, d := range diags {
			msgs = append(msgs, ast.Diagnostic_Localize(d, ast.DefaultLocale()))
		}
		return strings.Join(msgs, "\n")
	}

	t.Run("optional access then .has() - no diagnostic", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			declare const obj: { positions: Set<string> } | undefined;
			const result = obj?.positions.has("key") ?? false;
		`, Opts{})
		if hasDiag(diags, dw.UnsupportedBuiltinOptionalCall) {
			t.Errorf("should not diagnose non-optional builtin call in optional chain, got: %s", diagMessages(diags))
		}
	})

	t.Run("optional access then .push() - no diagnostic", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			declare const arr: number[] | undefined;
			arr?.push(1);
		`, Opts{})
		if hasDiag(diags, dw.UnsupportedBuiltinOptionalCall) {
			t.Errorf("should not diagnose non-optional builtin call in optional chain, got: %s", diagMessages(diags))
		}
	})

	t.Run("chained optional access then method call - no diagnostic", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			declare const obj: { inner?: { items: string[] } };
			obj.inner?.items.includes("x");
		`, Opts{})
		if hasDiag(diags, dw.UnsupportedBuiltinOptionalCall) {
			t.Errorf("should not diagnose non-optional builtin call in optional chain, got: %s", diagMessages(diags))
		}
	})
}
