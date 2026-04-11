import path from "path";
import type { TestCase } from "./types.ts";
import {
  goTargetConst,
  tstlEnumToTarget,
  supportedTstlDiagCodes,
  diagSnapshotSkips,
  codegenAssertionSkips,
  diagCodeSkips,
} from "./constants.ts";
import { serialize } from "./serialize.ts";
import { fullTsCode } from "./tstl-ref.ts";

export function goString(s: string): string {
  // Raw backtick strings cannot preserve CR (goimports strips bare \r from
  // Go source) or other control characters, so fall back to escaped form.
  if (!s.includes("`") && !s.includes("\0") && !s.includes("\r") && !s.includes("\uFEFF"))
    return "`" + s + "`";
  return (
    '"' +
    s
      .replace(/\\/g, "\\\\")
      .replace(/"/g, '\\"')
      .replace(/\r/g, "\\r")
      .replace(/\n/g, "\\n")
      .replace(/\t/g, "\\t")
      // eslint-disable-next-line no-control-regex
      .replace(/\u0000/g, "\\x00")
      .replace(/\uFEFF/g, "\\uFEFF") +
    '"'
  );
}

export function specToTestName(specFilename: string): string {
  const base = path.basename(specFilename, ".spec.ts");
  return base
    .split(/[-_.]/)
    .map((w: string) => w.charAt(0).toUpperCase() + w.slice(1))
    .join("");
}

export function goOpts(tc: TestCase): string[] {
  const opts: string[] = [];
  if (tc.languageExtensions) {
    opts.push(`WithLanguageExtensions()`);
  }
  if (tc.extraFiles) {
    for (const [name, code] of Object.entries(tc.extraFiles)) {
      opts.push(`WithExtraFile(${JSON.stringify(name)}, ${goString(code)})`);
    }
  }
  if (tc.returnExport && tc.returnExport.length > 0) {
    const args = tc.returnExport.map((n) => JSON.stringify(n)).join(", ");
    opts.push(`WithReturnExport(${args})`);
  }
  if (tc.luaHeader) {
    opts.push(`WithLuaHeader(${goString(tc.luaHeader)})`);
  }
  if (tc.luaFactory) {
    opts.push(`WithLuaFactory(${tc.luaFactory})`);
  }
  if (tc.mainFileName) {
    opts.push(`WithMainFileName(${JSON.stringify(tc.mainFileName)})`);
  }
  if (tc.allowDiagnostics) {
    opts.push(`WithAllowDiagnostics()`);
  }
  if (tc.options && Object.keys(tc.options).length > 0) {
    const rawTarget = tc.options.luaTarget as string | undefined;
    const luaTarget =
      rawTarget && goTargetConst[rawTarget] ? rawTarget : (tstlEnumToTarget[rawTarget ?? ""] ?? "");
    if (luaTarget && goTargetConst[luaTarget]) {
      opts.push(`WithLuaTarget(${goTargetConst[luaTarget]})`);
    }
    const filtered = Object.fromEntries(
      Object.entries(tc.options)
        .filter(([k]) => k !== "luaTarget" && !(k === "types" && tc.languageExtensions))
        .map(([k, v]) => {
          // Normalize lib names: tsgo only accepts short forms ("esnext", "dom")
          if (k === "lib" && Array.isArray(v)) {
            return [k, v.map((s: unknown) => (typeof s === "string" ? normalizeLibName(s) : s))];
          }
          return [k, v];
        }),
    );
    if (Object.keys(filtered).length > 0) {
      const pairs = Object.entries(filtered)
        .map(([k, v]) => `${JSON.stringify(k)}: ${goValue(v)}`)
        .join(", ");
      opts.push(`WithOptions(map[string]any{${pairs}})`);
    }
  }
  return opts;
}

export function goValue(v: unknown): string {
  if (typeof v === "boolean") return v ? "true" : "false";
  if (typeof v === "number") return String(v);
  if (typeof v === "string") return JSON.stringify(v);
  if (Array.isArray(v)) {
    const elems = v.map((e) => goValue(e)).join(", ");
    return `[]any{${elems}}`;
  }
  return "nil";
}

// Normalize TypeScript lib names: "lib.esnext.d.ts" -> "esnext", "lib.dom.d.ts" -> "dom", etc.
// tsgo only accepts short-form lib names, unlike tsc which accepts both.
function normalizeLibName(name: string): string {
  return name.replace(/^lib\./, "").replace(/\.d\.ts$/, "");
}

function isBatchable(tc: TestCase): boolean {
  if (tc.options && Object.keys(tc.options).length > 0) return false;
  if (tc.luaHeader) return false;
  if (tc.languageExtensions) return false;
  return true;
}

export function generateGoTest(
  testName: string,
  cases: TestCase[],
  specPath: string,
): { evalCode: string; codegenCode: string } {
  // Split into batchable (no options) and individual (has options)
  // Cases with extraFiles/returnExport use batchRunTests directly.
  const batchExprs: TestCase[] = [];
  const batchFuncs: TestCase[] = [];
  const batchModules: TestCase[] = [];
  const batchGeneral: TestCase[] = []; // has extraFiles or returnExport but no options
  const batchDiags: TestCase[] = []; // diagnostic tests with mapped codes
  const batchCodegen: TestCase[] = []; // codegen assertion tests (compile-only)
  const individual: TestCase[] = [];

  for (const tc of cases) {
    // Collect codegen assertions separately (emitted as compile-only tests)
    // Skip codegen assertions for luaLibImport modes whose codegen tslua does
    // not produce (only require / require-minimal share the same per-file form)
    const specBase = path.basename(specPath, ".spec.ts");
    const skipKey = `${specBase}::${tc.name}`;
    if (
      tc.codegen &&
      (!tc.options?.luaLibImport ||
        tc.options.luaLibImport === "require" ||
        tc.options.luaLibImport === "require-minimal")
    ) {
      // Skip codegen assertions for tests with intentional differences
      if (codegenAssertionSkips.has(skipKey)) {
        // still push but with cleared assertions
        batchCodegen.push({ ...tc, codegen: { contains: undefined, notContains: undefined } });
      } else {
        batchCodegen.push(tc);
      }
    }
    // Codegen-only tests (no runtime assertion) — skip runtime batching
    if (tc.assertion === "other" || tc.assertion === "snapshot-resolved") continue;
    if (tc.assertion === "diagnostic") {
      // Pass through TSTL codes that tslua supports; keep non-TSTL codes (e.g. TS compiler codes) as-is
      // Only keep TSTL diagnostic codes (100xxx) that tslua supports.
      // TS compiler codes (< 100000) are filtered out because our test infrastructure
      // only checks transpiler diagnostics, not TS pre-emit diagnostics.
      let filteredCodes = (tc.expectedDiagCodes || []).filter((c) => supportedTstlDiagCodes.has(c));
      // Remove specific diagnostic codes that tslua emits differently
      const diagSkip = diagCodeSkips.get(skipKey);
      if (diagSkip) {
        filteredCodes = filteredCodes.filter((c) => !diagSkip.has(c));
      }
      batchDiags.push({
        ...tc,
        expectedDiagCodes: filteredCodes.length > 0 ? filteredCodes : undefined,
      });
      continue;
    }
    if (!isBatchable(tc)) {
      individual.push(tc);
    } else if (tc.extraFiles || tc.returnExport || tc.tsHeader || tc.mainFileName) {
      batchGeneral.push(tc);
    } else if (tc.mode === "expression") {
      batchExprs.push(tc);
    } else if (tc.mode === "function") {
      batchFuncs.push(tc);
    } else {
      batchModules.push(tc);
    }
  }

  // Only import transpiler package if we emit WithLuaTarget() in runtime/individual/diagnostic/snapshot tests
  const snapshotResolved = cases.filter((tc) => tc.assertion === "snapshot-resolved");
  const needsTranspiler =
    individual.some((tc) => tc.options?.luaTarget) ||
    batchCodegen.some((tc) => tc.options?.luaTarget) ||
    batchDiags.some((tc) => tc.options?.luaTarget) ||
    snapshotResolved.some((tc) => tc.options?.luaTarget);

  const lines: string[] = [];
  lines.push(`// Code generated by scripts/migrate from ${specPath}. DO NOT EDIT.`);
  lines.push(`package tstltest`);
  lines.push(``);
  if (needsTranspiler) {
    lines.push(`import (`);
    lines.push(`\t"testing"`);
    lines.push(``);
    lines.push(`\t"github.com/realcoldfry/tslua/internal/transpiler"`);
    lines.push(`)`);
  } else {
    lines.push(`import "testing"`);
  }
  lines.push(``);
  lines.push(`func TestEval_${testName}(t *testing.T) {`);
  lines.push(`\tt.Parallel()`);

  // Emit batch calls for each mode
  if (batchExprs.length > 0) {
    lines.push(``);
    lines.push(`\tbatchExpectExpressions(t, []exprTestCase{`);
    for (const tc of batchExprs) {
      lines.push(
        `\t\t{${JSON.stringify(tc.name)}, ${goString(tc.tsCode)}, ${goString(serialize(tc.expectedValue))}, ${goString(tc.refLua ?? "")}, ${tc.allowErrors ? "true" : "false"}, ${tc.allowDiagnostics ? "true" : "false"}},`,
      );
    }
    lines.push(`\t})`);
  }

  if (batchFuncs.length > 0) {
    lines.push(``);
    lines.push(`\tbatchExpectFunctions(t, []funcTestCase{`);
    for (const tc of batchFuncs) {
      lines.push(
        `\t\t{${JSON.stringify(tc.name)}, ${goString(tc.tsCode)}, ${goString(serialize(tc.expectedValue))}, ${goString(tc.refLua ?? "")}, ${tc.allowErrors ? "true" : "false"}, ${tc.allowDiagnostics ? "true" : "false"}},`,
      );
    }
    lines.push(`\t})`);
  }

  if (batchModules.length > 0) {
    lines.push(``);
    lines.push(`\tbatchExpectModules(t, []moduleTestCase{`);
    for (const tc of batchModules) {
      lines.push(
        `\t\t{${JSON.stringify(tc.name)}, ${goString(tc.tsCode)}, ${goString(serialize(tc.expectedValue))}, ${goString(tc.refLua ?? "")}, ${tc.allowErrors ? "true" : "false"}, ${tc.allowDiagnostics ? "true" : "false"}},`,
      );
    }
    lines.push(`\t})`);
  }

  // Emit batchRunTests for cases with extraFiles/returnExport/mainFileName
  if (batchGeneral.length > 0) {
    // Check if any case involves JSON files (mainFileName or extraFiles with .json)
    const needsJson = batchGeneral.some(
      (tc) =>
        tc.mainFileName?.endsWith(".json") ||
        (tc.extraFiles && Object.keys(tc.extraFiles).some((k) => k.endsWith(".json"))),
    );
    const batchOpts: string[] = [];
    if (needsJson) {
      batchOpts.push(`WithOptions(map[string]any{"resolveJsonModule": true})`);
    }
    const batchOptStr = batchOpts.length > 0 ? `, ${batchOpts.join(", ")}` : "";
    lines.push(``);
    lines.push(`\tbatchRunTests(t, []batchTestCase{`);
    for (const tc of batchGeneral) {
      const header = tc.tsHeader ? tc.tsHeader + "\n" : "";
      const tsCode =
        tc.mode === "expression"
          ? `${header}export const __result = ${tc.tsCode};`
          : tc.mode === "function"
            ? `${header}export function __main() {${tc.tsCode}}`
            : header + tc.tsCode;
      const accessor =
        tc.mode === "expression"
          ? `mod["__result"]`
          : tc.mode === "function"
            ? "mod.__main()"
            : tc.returnExport
              ? tc.returnExport.reduce((acc, name) => `${acc}[${JSON.stringify(name)}]`, "mod")
              : "mod";
      let entry = `{name: ${JSON.stringify(tc.name)}, tsCode: ${goString(tsCode)}, accessor: ${goString(accessor)}, want: ${goString(serialize(tc.expectedValue))}, refLua: ${goString(tc.refLua ?? "")}`;
      if (tc.extraFiles && Object.keys(tc.extraFiles).length > 0) {
        const pairs = Object.entries(tc.extraFiles)
          .map(([k, v]) => `${JSON.stringify(k)}: ${goString(v)}`)
          .join(", ");
        entry += `, extraFiles: map[string]string{${pairs}}`;
      }
      if (tc.allowErrors) {
        entry += `, allowErrors: true`;
      }
      if (tc.allowDiagnostics) {
        entry += `, allowDiagnostics: true`;
      }
      const ep = tc.entryPoint || tc.mainFileName;
      if (ep) {
        entry += `, entryPoint: ${JSON.stringify(ep)}`;
      }
      entry += `},`;
      lines.push(`\t\t${entry}`);
    }
    lines.push(`\t}${batchOptStr})`);
  }

  // Emit diagnostic tests, grouped by luaTarget
  if (batchDiags.length > 0) {
    const byTarget = new Map<string, TestCase[]>();
    for (const tc of batchDiags) {
      const key = (tc.options?.luaTarget as string) ?? "";
      if (!byTarget.has(key)) byTarget.set(key, []);
      byTarget.get(key)!.push(tc);
    }
    for (const [target, tcs] of byTarget) {
      lines.push(``);
      const goConst = target ? goTargetConst[target] : undefined;
      const opts: string[] = [];
      if (goConst) opts.push(`WithLuaTarget(${goConst})`);
      if (tcs.some((tc) => tc.languageExtensions)) opts.push(`WithLanguageExtensions()`);
      const optStr = opts.length > 0 ? `, ${opts.join(", ")}` : "";
      lines.push(`\tbatchExpectDiagnostics(t, []diagTestCase{`);
      for (const tc of tcs) {
        const codesStr = tc.expectedDiagCodes
          ? `[]int32{${tc.expectedDiagCodes.join(", ")}}`
          : "nil";
        // Fold tsHeader into tsCode — diagTestCase has no header field.
        // For expression mode, wrap as module with `export const __result = ...`.
        let mode = tc.mode;
        let code = tc.tsCode;
        if (tc.tsHeader) {
          if (mode === "expression") {
            code = `${tc.tsHeader}\nexport const __result = ${tc.tsCode};`;
            mode = "module";
          } else {
            code = `${tc.tsHeader}\n${tc.tsCode}`;
          }
        }
        const specBase = path.basename(specPath, ".spec.ts");
        const skipKey = `${specBase}::${tc.name}`;
        const codegenStr =
          tc.codegen?.snapshot && !diagSnapshotSkips.has(skipKey)
            ? `[]string{${goString(tc.codegen.snapshot)}}`
            : "nil";
        lines.push(
          `\t\t{${JSON.stringify(tc.name)}, ${JSON.stringify(mode)}, ${goString(code)}, ${codesStr}, ${codegenStr}},`,
        );
      }
      lines.push(`\t}${optStr})`);
    }
  }

  // Codegen snapshots from diagnostic tests are checked inline via
  // the wantCodegen field in diagTestCase — no separate batch needed.

  // Emit codegen assertion tests (compile-only, check Lua output shape)
  // Group by options so each batch compiles with the same settings
  if (batchCodegen.length > 0) {
    const byOpts = new Map<string, TestCase[]>();
    for (const tc of batchCodegen) {
      const key = JSON.stringify({
        target: (tc.options?.luaTarget as string) ?? "",
        langExt: !!tc.languageExtensions,
      });
      if (!byOpts.has(key)) byOpts.set(key, []);
      byOpts.get(key)!.push(tc);
    }
    for (const [, tcs] of byOpts) {
      const sample = tcs[0];
      const opts: string[] = [];
      const rawTarget = (sample.options?.luaTarget as string) ?? "";
      const normalizedTarget = goTargetConst[rawTarget]
        ? rawTarget
        : (tstlEnumToTarget[rawTarget] ?? "");
      if (normalizedTarget && goTargetConst[normalizedTarget]) {
        opts.push(`WithLuaTarget(${goTargetConst[normalizedTarget]})`);
      }
      if (sample.languageExtensions) {
        opts.push(`WithLanguageExtensions()`);
      }
      const optStr = opts.length > 0 ? `, ${opts.join(", ")}` : "";
      lines.push(``);
      lines.push(`\tbatchExpectCodegen(t, []codegenTestCase{`);
      for (const tc of tcs) {
        const containsStr = tc.codegen!.contains
          ? `[]string{${tc.codegen!.contains.map((s) => goString(s)).join(", ")}}`
          : "nil";
        const notContainsStr = tc.codegen!.notContains
          ? `[]string{${tc.codegen!.notContains.map((s) => goString(s)).join(", ")}}`
          : "nil";
        const matchesStr = tc.codegen!.matches
          ? `[]string{${tc.codegen!.matches.map((s) => goString(s)).join(", ")}}`
          : "nil";
        const notMatchesStr = tc.codegen!.notMatches
          ? `[]string{${tc.codegen!.notMatches.map((s) => goString(s)).join(", ")}}`
          : "nil";
        lines.push(
          `\t\t{${JSON.stringify(tc.name)}, ${JSON.stringify(tc.mode)}, ${goString(tc.tsCode)}, ${containsStr}, ${notContainsStr}, ${matchesStr}, ${notMatchesStr}},`,
        );
      }
      lines.push(`\t}` + optStr + `)`);
    }
  }

  // Emit snapshot codegen tests (exact match against TSTL reference output)
  const snapshotCases = cases.filter((tc) => tc.assertion === "snapshot-resolved" && tc.refLua);
  if (snapshotCases.length > 0) {
    const byOpts = new Map<string, TestCase[]>();
    for (const tc of snapshotCases) {
      const key = JSON.stringify(goOpts(tc));
      if (!byOpts.has(key)) byOpts.set(key, []);
      byOpts.get(key)!.push(tc);
    }
    for (const [, tcs] of byOpts) {
      const opts = goOpts(tcs[0]);
      const optStr = opts.length > 0 ? `, ${opts.join(", ")}` : "";
      lines.push(``);
      lines.push(`\tbatchCompareCodegen(t, []batchTestCase{`);
      for (const tc of tcs) {
        const header = tc.tsHeader ? tc.tsHeader + "\n" : "";
        const tsCode =
          tc.mode === "expression"
            ? `${header}export const __result = ${tc.tsCode};`
            : tc.mode === "function"
              ? `${header}export function __main() {${tc.tsCode}}`
              : header + tc.tsCode;
        const bugRef = tc.tstlBug;
        const bugField = bugRef ? `, tstlBug: ${goString(bugRef)}` : "";
        lines.push(
          `\t\t{name: ${JSON.stringify(tc.name)}, tsCode: ${goString(tsCode)}, refLua: ${goString(tc.refLua ?? "")}${bugField}},`,
        );
      }
      lines.push(`\t}${optStr})`);
    }
  }

  // Emit individual tests for cases with options
  for (const tc of individual) {
    const opts = goOpts(tc);
    const optsStr = opts.length > 0 ? ", " + opts.join(", ") : "";

    // Use approx comparison for non-integer floats (native Lua libm may differ from JS)
    const useApprox =
      tc.mode === "expression" &&
      typeof tc.expectedValue === "number" &&
      !Number.isInteger(tc.expectedValue) &&
      Number.isFinite(tc.expectedValue);

    // Expressions with tsHeader can't use expectExpression (it wraps code in
    // `export const __result = ...` which breaks the header). Use expectModule instead.
    // Functions with tsHeader: build the full source with header outside the function wrapper.
    const needsModuleForExprHeader = tc.mode === "expression" && tc.tsHeader;
    const needsFunctionRewrite = tc.mode === "function" && tc.tsHeader;
    const helper = useApprox
      ? "expectExpressionApprox"
      : needsModuleForExprHeader
        ? "expectModule"
        : tc.mode === "expression"
          ? "expectExpression"
          : tc.mode === "function"
            ? "expectFunction"
            : "expectModule";
    const wantArg = useApprox ? String(tc.expectedValue) : goString(serialize(tc.expectedValue));
    let tsCode: string;
    if (needsModuleForExprHeader) {
      tsCode = `${tc.tsHeader}\nexport const __result = ${tc.tsCode};`;
      // Add WithReturnExport for the module helper
      const retExpOpt = `, WithReturnExport("__result")`;
      const fullOpts = opts.length > 0 ? ", " + opts.join(", ") + retExpOpt : retExpOpt;
      lines.push(``);
      lines.push(`\tt.Run(${JSON.stringify(tc.name)}, func(t *testing.T) {`);
      lines.push(`\t\tt.Parallel()`);
      lines.push(`\t\t${helper}(t, ${goString(tsCode)}, ${wantArg}${fullOpts})`);
      lines.push(`\t})`);
    } else if (needsFunctionRewrite) {
      // Build full source with header at file level, body in function.
      // expectFunction detects the pre-wrapped "export function __main()" and uses it as-is.
      tsCode = `${tc.tsHeader}\nexport function __main() {${tc.tsCode}}`;
      lines.push(``);
      lines.push(`\tt.Run(${JSON.stringify(tc.name)}, func(t *testing.T) {`);
      lines.push(`\t\tt.Parallel()`);
      lines.push(`\t\t${helper}(t, ${goString(tsCode)}, ${wantArg}${optsStr})`);
      lines.push(`\t})`);
    } else {
      lines.push(``);
      lines.push(`\tt.Run(${JSON.stringify(tc.name)}, func(t *testing.T) {`);
      lines.push(`\t\tt.Parallel()`);
      lines.push(`\t\t${helper}(t, ${goString(fullTsCode(tc))}, ${wantArg}${optsStr})`);
      lines.push(`\t})`);
    }
  }

  lines.push(`}`);
  lines.push(``);

  // Generate codegen comparison file — grouped by target
  const codegenCmp = [
    ...batchExprs,
    ...batchFuncs,
    ...batchModules,
    ...batchGeneral,
    ...individual,
  ].filter((tc) => tc.refLua);
  let codegenCode = "";
  if (codegenCmp.length > 0) {
    // Group by luaTarget
    const byTarget = new Map<string, TestCase[]>();
    for (const tc of codegenCmp) {
      const target = (tc.options?.luaTarget as string) ?? "";
      if (!byTarget.has(target)) byTarget.set(target, []);
      byTarget.get(target)!.push(tc);
    }

    const needsCgTranspiler = [...byTarget.keys()].some((k) => k !== "");

    const cg: string[] = [];
    cg.push(`// Code generated by scripts/migrate from ${specPath}. DO NOT EDIT.`);
    cg.push(`package tstltest`);
    cg.push(``);
    if (needsCgTranspiler) {
      cg.push(`import (`);
      cg.push(`\t"testing"`);
      cg.push(``);
      cg.push(`\t"github.com/realcoldfry/tslua/internal/transpiler"`);
      cg.push(`)`);
    } else {
      cg.push(`import "testing"`);
    }
    cg.push(``);
    cg.push(`func TestCodegen_${testName}(t *testing.T) {`);
    cg.push(`\tt.Parallel()`);

    for (const [target, tcs] of byTarget) {
      const targetOpt =
        target && goTargetConst[target] ? `, WithLuaTarget(${goTargetConst[target]})` : "";
      cg.push(``);
      cg.push(`\tbatchCompareCodegen(t, []batchTestCase{`);
      for (const tc of tcs) {
        const header = tc.tsHeader ? tc.tsHeader + "\n" : "";
        const tsCode =
          tc.mode === "expression"
            ? `${header}export const __result = ${tc.tsCode};`
            : tc.mode === "function"
              ? `${header}export function __main() {${tc.tsCode}}`
              : header + tc.tsCode;
        const bugRef = tc.tstlBug;
        const bugField = bugRef ? `, tstlBug: ${goString(bugRef)}` : "";
        cg.push(
          `\t\t{name: ${JSON.stringify(tc.name)}, tsCode: ${goString(tsCode)}, refLua: ${goString(tc.refLua ?? "")}${bugField}},`,
        );
      }
      cg.push(`\t}${targetOpt})`);
    }

    cg.push(`}`);
    cg.push(``);
    codegenCode = cg.join("\n");
  }

  return { evalCode: lines.join("\n"), codegenCode };
}
