import fs from "fs";
import path from "path";
import type { TestCase, MigrateResult } from "./types.ts";
import { isInertProxy } from "./builder.ts";
import {
  tstlBugOverrides,
  tstlBugSkips,
  tstlBugCodegenSkips,
  bakeLimitationOverrides,
  batchDiagnosticOverrides,
  tstlEnumToTarget,
  supportedTargetValues,
} from "./constants.ts";
import { extractTestCases } from "./extract.ts";
import { evaluateJS } from "./evaluate.ts";
import { getTstlRefLua, resolveSnapshotCases } from "./tstl-ref.ts";
import { generateGoTest, specToTestName } from "./codegen.ts";

export function migrateSpec(specPath: string): MigrateResult {
  const { cases: allCases, extractionErrors } = extractTestCases(specPath);

  // Resolve snapshot assertions to codegen contains checks
  resolveSnapshotCases(allCases, specPath);

  const migratable: TestCase[] = [];
  const skippedCases: { name: string; reason: string }[] = [];
  for (const c of allCases) {
    if (
      c.assertion !== "matchJsResult" &&
      c.assertion !== "equal" &&
      c.assertion !== "diagnostic" &&
      c.assertion !== "snapshot-resolved"
    ) {
      if (!c.codegen) skippedCases.push({ name: c.name, reason: `assertion=${c.assertion}` });
      continue;
    }
    const rawTarget = c.options?.luaTarget as string | undefined;
    const luaTarget = rawTarget ? (tstlEnumToTarget[rawTarget] ?? rawTarget) : undefined;
    if (luaTarget && !supportedTargetValues.has(luaTarget)) {
      skippedCases.push({ name: c.name, reason: `unsupported target=${rawTarget}` });
      continue;
    }
    if (luaTarget && rawTarget !== luaTarget) {
      c.options!.luaTarget = luaTarget;
    }
    migratable.push(c);
  }

  // Also include codegen-only tests (tap assertions without runtime check)
  const codegenOnly = allCases.filter((c) => c.codegen && c.assertion === "other");

  // Bake JS expected values for matchJsResult cases
  const baseName = path.basename(specPath, ".spec.ts");
  const bakeErrors: string[] = [];
  for (const tc of migratable) {
    if (tc.assertion === "matchJsResult") {
      // Check if this case has a pre-baked override (e.g. declare const enum)
      const key = `${baseName}::${tc.name}`;
      const override = tstlBugOverrides[key];
      const bloOverride = bakeLimitationOverrides.find(
        (b) => b.key === key && tc.tsCode.includes(b.codeContains),
      );
      if (override) {
        tc.assertion = "equal";
        tc.expectedValue = override.expectedValue;
        tc.tstlBug = override.bug;
        continue;
      }
      if (bloOverride) {
        tc.assertion = "equal";
        tc.expectedValue = bloOverride.expectedValue;
        tc.tstlBug = bloOverride.bug;
        continue;
      }
      try {
        const result = evaluateJS(
          tc.mode,
          tc.tsCode,
          tc.extraFiles,
          tc.returnExport,
          tc.tsHeader,
          tc.languageExtensions,
          tc.options,
          tc.allowErrors,
        );
        tc.assertion = "equal";
        tc.expectedValue = result;
      } catch (e: any) {
        bakeErrors.push(`${tc.name}: ${e.message}`);
      }
    }
  }
  // Remove cases that failed to bake (keep diagnostic and codegen tests)
  const finalCases = [
    ...migratable.filter(
      (c) =>
        (c.assertion === "equal" && !isInertProxy(c.expectedValue)) ||
        c.assertion === "diagnostic" ||
        c.assertion === "snapshot-resolved",
    ),
    ...codegenOnly,
  ];

  // Apply TSTL bug overrides and skips
  let overrideCount = 0;
  for (let i = finalCases.length - 1; i >= 0; i--) {
    const tc = finalCases[i];
    const key = `${baseName}::${tc.name}`;

    // Full skip — remove from both eval and codegen
    const fullSkip = tstlBugSkips[key];
    if (fullSkip) {
      skippedCases.push({ name: tc.name, reason: `tstl-bug: ${fullSkip}` });
      finalCases.splice(i, 1);
      overrideCount++;
      continue;
    }

    // Override expected value (eval runs with corrected value, codegen skipped)
    const override = tstlBugOverrides[key];
    if (override) {
      tc.expectedValue = override.expectedValue;
      tc.tstlBug = override.bug;
      overrideCount++;
    }

    // Bake limitation overrides: match on name + code content
    for (const blo of bakeLimitationOverrides) {
      if (blo.key === key && tc.tsCode.includes(blo.codeContains)) {
        tc.expectedValue = blo.expectedValue;
        tc.tstlBug = blo.bug;
        overrideCount++;
        break;
      }
    }

    // Skip codegen only (eval still runs)
    const codegenSkip = tstlBugCodegenSkips[key];
    if (codegenSkip) {
      tc.tstlBug = codegenSkip;
      overrideCount++;
    }

    // Batch diagnostic override — our batching causes type conflicts
    if (batchDiagnosticOverrides.has(key)) {
      tc.allowDiagnostics = true;
    }
  }

  // Compute TSTL reference Lua for each non-diagnostic case
  for (const tc of finalCases) {
    if (tc.assertion === "diagnostic") continue;
    tc.refLua = getTstlRefLua(tc);
  }

  const codegenCount = finalCases.filter((c) => c.codegen).length;
  const diagCount = finalCases.filter((c) => c.assertion === "diagnostic").length;

  // Compact summary line
  const parts = [`${finalCases.length} cases`];
  if (codegenCount > 0) parts.push(`${codegenCount} codegen`);
  if (diagCount > 0) parts.push(`${diagCount} diag`);
  if (skippedCases.length > 0) parts.push(`${skippedCases.length} skipped`);
  if (overrideCount > 0) parts.push(`${overrideCount} tstl-bug overrides`);
  if (bakeErrors.length > 0) parts.push(`${bakeErrors.length} bake failed`);
  if (extractionErrors.length > 0) parts.push(`${extractionErrors.length} extraction failed`);

  const testName = specToTestName(specPath);
  const { evalCode, codegenCode } = generateGoTest(testName, finalCases, specPath);

  const evalFile = `internal/tstltest/${baseName}_test.go`;
  fs.writeFileSync(evalFile, evalCode);

  if (codegenCode) {
    const codegenFile = `internal/tstltest/${baseName}_codegen_test.go`;
    fs.writeFileSync(codegenFile, codegenCode);
  }

  return {
    specPath,
    outputFile: evalFile,
    summary: parts.join(", "),
    skippedCases,
    bakeErrors,
    extractionErrors,
  };
}
