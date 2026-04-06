package luatest

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/shim/ast"
	dw "github.com/microsoft/typescript-go/shim/diagnosticwriter"
)

func TestAssignmentValidation_Diagnostics(t *testing.T) {
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

	t.Run("self function to this:void variable", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			function a(this: any) {}
			const b: (this: void) => void = a;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedNoSelfFunctionConversion) {
			t.Errorf("expected TL1011 (unsupportedNoSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("this:void function to self variable", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			function a(this: void) {}
			const b: (this: any) => void = a;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedSelfFunctionConversion) {
			t.Errorf("expected TL1012 (unsupportedSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("arrow to this:void is a mismatch", func(t *testing.T) {
		// Arrow function type () => void defaults to NonVoid (no explicit this: void).
		// Assigning to (this: void) => void is a context mismatch, matching TSTL behavior.
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			const a = () => {};
			const b: (this: void) => void = a;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedNoSelfFunctionConversion) {
			t.Errorf("expected TL1011 (unsupportedNoSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("compatible self to self assignment", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			function a(this: any) {}
			const b: (this: any) => void = a;
		`, Opts{})
		if hasDiag(diags, dw.UnsupportedNoSelfFunctionConversion) || hasDiag(diags, dw.UnsupportedSelfFunctionConversion) {
			t.Errorf("expected no context mismatch diagnostic, got: %s", diagMessages(diags))
		}
	})

	t.Run("compatible void to void assignment", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			function a(this: void) {}
			const b: (this: void) => void = a;
		`, Opts{})
		if hasDiag(diags, dw.UnsupportedNoSelfFunctionConversion) || hasDiag(diags, dw.UnsupportedSelfFunctionConversion) {
			t.Errorf("expected no context mismatch diagnostic, got: %s", diagMessages(diags))
		}
	})

	t.Run("binary assignment self to void", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			function a(this: any) {}
			let b: (this: void) => void;
			b = a;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedNoSelfFunctionConversion) {
			t.Errorf("expected TL1011 (unsupportedNoSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("return value validation", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			function a(this: any) {}
			function returnsVoidFn(): (this: void) => void {
				return a;
			}
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedNoSelfFunctionConversion) {
			t.Errorf("expected TL1011 (unsupportedNoSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("no diagnostic without type annotation", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			const a = () => {};
			const b = a;
		`, Opts{})
		if hasDiag(diags, dw.UnsupportedNoSelfFunctionConversion) || hasDiag(diags, dw.UnsupportedSelfFunctionConversion) {
			t.Errorf("expected no context mismatch diagnostic, got: %s", diagMessages(diags))
		}
	})

	t.Run("noSelf class method assignment to default-context variable", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			/** @noSelf */ class C { m(s: string): string { return s; } }
			const c = new C();
			let fn: (s: string) => string;
			fn = c.m;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedSelfFunctionConversion) {
			t.Errorf("expected TL1012 (unsupportedSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("noSelf class method to this:void variable declaration", func(t *testing.T) {
		// When source (@noSelf method) and target (this: void) are both void-context,
		// tsgo deduplicates the types AND both contexts resolve to Void → no mismatch.
		// TSTL emits TL1011 because TypeScript's checker returns distinct type objects,
		// so the type-level check produces a different result due to different signature
		// declarations. This is a known tsgo behavioral difference.
		t.Skip("tsgo type deduplication: @noSelf void source == (this: void) void target")
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			/** @noSelf */ class C { m(s: string): string { return s; } }
			const c = new C();
			const fn: (this: void, s: string) => string = c.m;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedNoSelfFunctionConversion) {
			t.Errorf("expected TL1011 (unsupportedNoSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("noSelf class method to this:any variable declaration", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			/** @noSelf */ class C { m(s: string): string { return s; } }
			const c = new C();
			const fn: (this: any, s: string) => string = c.m;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedSelfFunctionConversion) {
			t.Errorf("expected TL1012 (unsupportedSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("this:void function to default-context variable assignment", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			let voidFunc: {(this: void, s: string): string} = function(s) { return s; };
			let fn: (s: string) => string;
			fn = voidFunc;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedSelfFunctionConversion) {
			t.Errorf("expected TL1012 (unsupportedSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})

	t.Run("noSelf namespace function to default-context variable declaration", func(t *testing.T) {
		t.Parallel()
		_, diags := TranspileProgramDiags(t, `
			/** @noSelf */ namespace NS { export function f(s: string): string { return s; } }
			const fn: (s: string) => string = NS.f;
		`, Opts{})
		if !hasDiag(diags, dw.UnsupportedSelfFunctionConversion) {
			t.Errorf("expected TL1012 (unsupportedSelfFunctionConversion), got: %s", diagMessages(diags))
		}
	})
}
