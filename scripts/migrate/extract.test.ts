import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { glob } from "fs/promises";
import { extractTestCases } from "./extract.ts";
import { supportedTstlDiagCodes } from "./constants.ts";

describe("extractTestCases", () => {
  test("extracts semicolons spec (simple snapshot tests)", () => {
    const { cases, extractionErrors } = extractTestCases(
      "extern/tstl/test/unit/printer/semicolons.spec.ts",
    );
    assert.equal(extractionErrors.length, 0);
    assert.ok(cases.length >= 8, `expected >= 8 cases, got ${cases.length}`);
    const assertions = new Set(cases.map((c) => c.assertion));
    assert.ok(assertions.has("matchJsResult"), "expected matchJsResult cases");
    assert.ok(assertions.has("snapshot"), "expected snapshot cases");
  });

  test("extracts array spec (expressions, functions, diagnostics)", () => {
    const { cases, extractionErrors } = extractTestCases(
      "extern/tstl/test/unit/builtins/array.spec.ts",
    );
    assert.ok(cases.length >= 200, `expected >= 200 cases, got ${cases.length}`);
    // Should have a mix of assertion types
    const assertions = new Set(cases.map((c) => c.assertion));
    assert.ok(assertions.has("matchJsResult") || assertions.has("equal"));
    assert.ok(assertions.has("diagnostic"));
    // Extraction errors should be minimal (some tests use getLuaExecutionResult)
    assert.ok(extractionErrors.length <= 5, `expected <= 5 errors, got ${extractionErrors.length}`);
  });

  test("loads relative test helper files (functionPermutations)", () => {
    const { cases, extractionErrors } = extractTestCases(
      "extern/tstl/test/unit/functions/validation/validFunctionAssignments.spec.ts",
    );
    assert.equal(extractionErrors.length, 0);
    // This spec uses test.each with Cartesian products of function permutations
    assert.ok(cases.length >= 100, `expected >= 100 cases, got ${cases.length}`);
  });

  test("handles testEachVersion (multi-target tests)", () => {
    const { cases, extractionErrors: _extractionErrors } = extractTestCases(
      "extern/tstl/test/unit/loops.spec.ts",
    );
    assert.ok(cases.length >= 50, `expected >= 50 cases, got ${cases.length}`);
    // Should have cases with luaTarget options
    const withTarget = cases.filter((c) => c.options?.luaTarget);
    assert.ok(withTarget.length > 0, "expected some tests with luaTarget");
  });

  test("handles language extensions", () => {
    const { cases, extractionErrors: _extractionErrors } = extractTestCases(
      "extern/tstl/test/unit/language-extensions/table.spec.ts",
    );
    assert.ok(cases.length >= 10, `expected >= 10 cases, got ${cases.length}`);
    const withLangExt = cases.filter((c) => c.languageExtensions);
    assert.ok(withLangExt.length > 0, "expected some tests with languageExtensions");
  });

  test("captures extraction errors for unsupported patterns", () => {
    // file.spec.ts has a test that accesses [0] on undefined
    const { extractionErrors } = extractTestCases("extern/tstl/test/unit/file.spec.ts");
    assert.ok(extractionErrors.length >= 1, "expected at least 1 extraction error");
  });

  test("handles extra files and return exports", () => {
    const { cases } = extractTestCases("extern/tstl/test/unit/modules/modules.spec.ts");
    const withExtra = cases.filter((c) => c.extraFiles && Object.keys(c.extraFiles).length > 0);
    assert.ok(withExtra.length > 0, "expected some tests with extraFiles");
    const withReturn = cases.filter((c) => c.returnExport && c.returnExport.length > 0);
    assert.ok(withReturn.length > 0, "expected some tests with returnExport");
  });

  test("handles diagnostic tests", () => {
    const { cases } = extractTestCases("extern/tstl/test/unit/identifiers.spec.ts");
    const diags = cases.filter((c) => c.assertion === "diagnostic");
    assert.ok(diags.length >= 10, `expected >= 10 diagnostic cases, got ${diags.length}`);
  });
});

describe("diagnostic code coverage", () => {
  test("reports unmapped TSTL diagnostic codes used in specs", async () => {
    const allCodes = new Set<number>();
    const codeToSpecs = new Map<number, string[]>();

    for await (const specPath of glob("extern/tstl/test/unit/**/*.spec.ts")) {
      const { cases } = extractTestCases(specPath);
      for (const c of cases) {
        if (c.assertion === "diagnostic" && c.expectedDiagCodes) {
          for (const code of c.expectedDiagCodes) {
            allCodes.add(code);
            const specs = codeToSpecs.get(code) ?? [];
            specs.push(specPath);
            codeToSpecs.set(code, specs);
          }
        }
      }
    }

    const unsupported = [...allCodes]
      .filter((c) => c >= 100000 && !supportedTstlDiagCodes.has(c))
      .toSorted((a, b) => a - b);

    // Report as a progress summary rather than a hard failure
    if (unsupported.length > 0) {
      console.error(`\n  Unsupported TSTL diagnostic codes (${unsupported.length}):`);
      for (const code of unsupported) {
        const specs = [...new Set(codeToSpecs.get(code)!)].map((s) =>
          s.replace("extern/tstl/test/unit/", ""),
        );
        console.error(`    ${code} → used in: ${specs.join(", ")}`);
      }
      console.error(
        `  Supported: ${supportedTstlDiagCodes.size}, Unsupported: ${unsupported.length}, Total: ${allCodes.size}\n`,
      );
    }

    // Track progress: assert we don't regress below current supported count
    assert.ok(
      supportedTstlDiagCodes.size >= 9,
      `expected at least 9 supported diagnostic codes, got ${supportedTstlDiagCodes.size}`,
    );
  });
});
