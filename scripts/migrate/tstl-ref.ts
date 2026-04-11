import ts from "typescript";
import fs from "fs";
import path from "path";
import crypto from "crypto";
import { transpileVirtualProject, LuaTarget, LuaLibImportKind } from "../../extern/tstl/dist/index";
import type { TestCase } from "./types.ts";

// Returns the full TS source for a test case (header + code).
export function fullTsCode(tc: TestCase): string {
  return tc.tsHeader ? tc.tsHeader + "\n" + tc.tsCode : tc.tsCode;
}

// ---- TSTL reference Lua cache ----

const refLuaCachePath = ".cache/tstl-ref-lua-cache.json";
export let refLuaCache: Record<string, string> = {};
let refLuaCacheDirty = false;

export function loadRefLuaCache(): void {
  try {
    refLuaCache = JSON.parse(fs.readFileSync(refLuaCachePath, "utf-8"));
  } catch {
    refLuaCache = {};
  }
}

export function saveRefLuaCache(): void {
  if (!refLuaCacheDirty) return;
  fs.mkdirSync(path.dirname(refLuaCachePath), { recursive: true });
  fs.writeFileSync(refLuaCachePath, JSON.stringify(refLuaCache, null, 2));
}

export function setCacheEntry(key: string, lua: string): void {
  refLuaCache[key] = lua;
  refLuaCacheDirty = true;
}

export function refLuaCacheKey(
  fullCode: string,
  luaTarget: string,
  lib?: string[],
  languageExtensions?: boolean,
): string {
  const libKey = lib ? lib.join(",") : "";
  // Append LE marker only when enabled so non-LE keys stay identical to
  // existing cache entries (backward compatible, no mass re-fetch).
  const leKey = languageExtensions ? "\0le" : "";
  return crypto
    .createHash("sha256")
    .update(`${luaTarget}\0${libKey}\0${fullCode}${leKey}`)
    .digest("hex");
}

// Absolute path to TSTL's language-extensions types directory. Mirrors
// extern/tstl/test/util.ts:175, which does path.resolve(__dirname, "..",
// "language-extensions"). The migration script runs from repo root.
const langExtTypesPath = path.resolve("extern/tstl/language-extensions");

// Transpile a test case's TS code with TSTL and return the reference Lua output.
export function getTstlRefLua(tc: TestCase): string {
  const header = tc.tsHeader ? tc.tsHeader + "\n" : "";
  let fullCode: string;
  switch (tc.mode) {
    case "expression":
      fullCode = `${header}export const __result = ${tc.tsCode};`;
      break;
    case "function":
      fullCode = `${header}export function __main() {${tc.tsCode}}`;
      break;
    case "module":
      fullCode = header + tc.tsCode;
      break;
  }

  const luaTarget = (tc.options?.luaTarget as string) ?? "5.5";
  // Per-test lib override (e.g., console tests need lib.dom.d.ts).
  // Default matches TSTL test helper (test/util.ts:172).
  const lib = (tc.options?.lib as string[]) ?? ["lib.esnext.d.ts"];
  const languageExtensions = tc.languageExtensions === true;
  const key = refLuaCacheKey(fullCode, luaTarget, lib, languageExtensions);

  if (key in refLuaCache) {
    return refLuaCache[key];
  }

  // When LE is enabled, mirror TSTL's test util by appending the
  // language-extensions types path to compiler options.
  const existingTypes = (tc.options?.types as string[] | undefined) ?? [];
  const types = languageExtensions ? [...existingTypes, langExtTypesPath] : existingTypes;

  try {
    const result = transpileVirtualProject(
      { "main.ts": fullCode },
      {
        luaTarget: luaTarget as LuaTarget,
        luaLibImport: LuaLibImportKind.Require,
        noHeader: true,
        target: ts.ScriptTarget.ES2017, // Match TSTL test helper default
        lib,
        ...(types.length > 0 ? { types } : {}),
      },
    );
    const mainFile = result.transpiledFiles.find((f) => f.outPath === "main.lua");
    const lua = mainFile?.lua?.trimEnd() ?? "";
    setCacheEntry(key, lua);
    return lua;
  } catch {
    setCacheEntry(key, "");
    return "";
  }
}

// ---- Snapshot parsing ----

export function parseSnapshots(specPath: string): Map<string, string> {
  const dir = path.dirname(specPath);
  const base = path.basename(specPath);
  const snapPath = path.join(dir, "__snapshots__", base + ".snap");
  const entries = new Map<string, string>();
  if (!fs.existsSync(snapPath)) return entries;
  const content = fs.readFileSync(snapPath, "utf-8");
  const re = /exports\[`(.+?)`\]\s*=\s*`\n?([\s\S]*?)`;\n/g;
  let m;
  while ((m = re.exec(content)) !== null) {
    let value = m[2];
    // Strip surrounding quotes that TSTL adds
    value = value
      .replace(/^"/, "")
      .replace(/"(\s*)$/, "$1")
      .trimEnd();
    // Unescape template literal escaping in keys (\\\" → \", \\\\ → \\)
    const key = m[1].replace(/\\\\/g, "\\");
    entries.set(key, value);
  }
  return entries;
}

export function resolveSnapshotCases(cases: TestCase[], specPath: string): void {
  const hasSnapshots = cases.some(
    (c) => c.assertion === "snapshot" || c.assertion === "diagnostic",
  );
  if (!hasSnapshots) return;
  const snapshots = parseSnapshots(specPath);
  // Jest snapshot keys use incrementing suffix per test name: "name 1", "name 2", etc.
  const nameCounters = new Map<string, number>();
  for (const tc of cases) {
    if (tc.assertion === "snapshot") {
      const count = (nameCounters.get(tc.name) ?? 0) + 1;
      nameCounters.set(tc.name, count);
      const snapKey = `${tc.name} ${count}`;
      if (snapshots.has(snapKey)) {
        // Mark as resolved snapshot: codegen-only, gets refLua via getTstlRefLua
        // and emitted as batchCompareCodegen in TestEval for exact output match.
        tc.assertion = "snapshot-resolved";
      }
    } else if (tc.assertion === "diagnostic") {
      // expectDiagnosticsToMatchSnapshot emits two snapshots: "diagnostics" and "code".
      // The "code" snapshot contains the expected Lua codegen — attach it as a codegen assertion.
      const codeKey = `${tc.name}: code 1`;
      const codeSnap = snapshots.get(codeKey);
      if (codeSnap) {
        if (!tc.codegen) tc.codegen = {};
        // Store the full expected codegen for exact-match comparison
        tc.codegen.snapshot = codeSnap;
      }
    }
  }
}
