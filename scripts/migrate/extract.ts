import ts from "typescript";
import vm from "vm";
import fs from "fs";
import path from "path";
import type { TestCase, ExtractionError, SandboxRef } from "./types.ts";
import { taggedTemplate, LUA_CODE_BRAND } from "./builder.ts";
import { supportedTargets, tstlTargetMap, diagFactory } from "./constants.ts";
import { stringContaining } from "./serialize.ts";

// Proxy that returns a no-op function for any property access — used for expect() matchers
// so that any Jest matcher (toBe, toEqual, toContain, toBeGreaterThanOrEqual, etc.) is a no-op.
function noopMatcherProxy(): any {
  return new Proxy(() => {}, {
    get(_target, prop) {
      if (prop === "not" || prop === "resolves" || prop === "rejects") return noopMatcherProxy();
      return () => {};
    },
  });
}

// Swallow rejected promises from async test callbacks so they don't become
// unhandled rejections. We only care about synchronous side effects (builder calls).
function swallowAsync(result: unknown): void {
  if (result && typeof (result as any).catch === "function") {
    (result as Promise<unknown>).catch(() => {});
  }
}

export function extractTestCases(specPath: string): {
  cases: TestCase[];
  extractionErrors: ExtractionError[];
} {
  const cases: TestCase[] = [];
  const sandboxRef: SandboxRef = { current: null };

  const util = {
    testExpression: taggedTemplate("expression", cases, sandboxRef),
    testFunction: taggedTemplate("function", cases, sandboxRef),
    testModule: taggedTemplate("module", cases, sandboxRef),
    testExpressionTemplate: taggedTemplate("expression", cases, sandboxRef, true),
    testFunctionTemplate: taggedTemplate("function", cases, sandboxRef, true),
    testModuleTemplate: taggedTemplate("module", cases, sandboxRef, true),
    testBundle: taggedTemplate("module", cases, sandboxRef),
    formatCode: (...values: unknown[]) =>
      values
        .map((v) => {
          if (typeof v === "function") return v.toString();
          if (v === undefined) return "undefined";
          if (typeof v === "number" && isNaN(v)) return "NaN";
          return JSON.stringify(v);
        })
        .join(", "),
    testEachVersion: (
      name: string | undefined,
      common: () => any,
      special?: Record<string, ((builder: any) => void) | boolean>,
    ) => {
      for (const target of supportedTargets) {
        // Map our target back to TSTL enum value for lookup in special map
        const tstlValue = Object.entries(tstlTargetMap).find(([, v]) => v === target)?.[0];
        // We run universal on lua 5.1, so inherit 5.1's override when universal wasn't
        // explicitly overridden. TSTL runs universal on 5.4 which masks 5.1 incompatibilities
        // (e.g. coroutine.yield in pcall).
        let specialEntry = tstlValue ? special?.[tstlValue] : undefined;
        if (target === "universal" && special && "Lua 5.1" in special) {
          // We run universal on lua 5.1, so inherit 5.1's override when universal
          // wasn't explicitly overridden. Detect "default" entries (from
          // expectEachVersionExceptJit) by comparing references to another target
          // that's unlikely to be individually overridden.
          const universalEntry = special["universal"];
          const referenceEntry = special["Lua 5.3"] ?? special["Lua 5.4"];
          const isDefaultEntry = universalEntry === referenceEntry;
          if (isDefaultEntry) {
            specialEntry = special["Lua 5.1"];
          }
        }
        if (specialEntry === false) continue;

        const testName = name === undefined ? target : `${name} [${target}]`;
        const before = cases.length;
        try {
          const builder = common();
          builder.setOptions({ luaTarget: target });
          if (typeof specialEntry === "function") {
            specialEntry(builder);
          }
        } catch (e: any) {
          if (cases.length === before) {
            extractionErrors.push({ name: testName, error: e.message ?? String(e) });
          }
        }
        for (let i = before; i < cases.length; i++) {
          cases[i].name = testName;
        }
      }
    },
    // TSTL skips JIT here because JIT is tested by non-versioned tests.
    // We include JIT since we want all supported targets tested explicitly.
    expectEachVersionExceptJit: (expectation: (builder: any) => void) => {
      const result: Record<string, ((builder: any) => void) | boolean> = {};
      for (const tstlValue of Object.keys(tstlTargetMap)) {
        result[tstlValue] = expectation;
      }
      return result;
    },
  };

  // Stack of describe() names for Jest-style nested test naming
  const describeStack: string[] = [];
  const extractionErrors: ExtractionError[] = [];

  const testFn: any = (name: string, fn: () => void) => {
    const before = cases.length;
    try {
      swallowAsync(fn());
    } catch (e: any) {
      const prefix = describeStack.length > 0 ? describeStack.join(" ") + " " : "";
      if (cases.length === before) {
        extractionErrors.push({ name: prefix + name, error: e.message ?? String(e) });
      }
    }
    const prefix = describeStack.length > 0 ? describeStack.join(" ") + " " : "";
    for (let i = before; i < cases.length; i++) {
      cases[i].name = prefix + name;
    }
  };

  testFn.each = (values: unknown[] | unknown) => {
    if (!values || typeof values !== "object" || !(Symbol.iterator in (values as any))) {
      // Jest's test.each also accepts tagged template literals — not supported, return no-op
      return () => {};
    }
    // Jest heuristic: if the first element is an array, treat as 2D table format
    // (each inner array is spread as arguments). Otherwise, 1D format (each value
    // is passed as a single argument).
    const valuesArr = values as unknown[];
    const is2D = valuesArr.length > 0 && Array.isArray(valuesArr[0]);
    return (nameTemplate: string, fn: (...args: unknown[]) => void) => {
      for (const value of valuesArr) {
        const args = is2D && Array.isArray(value) ? value : [value];
        let name = nameTemplate;
        let argIdx = 0;
        name = name.replace(/%[psi]/g, (fmt) => {
          const arg = args[argIdx++];
          // %p uses Jest's pretty-format: special numbers render as their name,
          // not "null" like JSON.stringify would produce.
          if (fmt === "%p") {
            if (typeof arg === "number") {
              if (Number.isNaN(arg)) return "NaN";
              if (arg === Infinity) return "Infinity";
              if (arg === -Infinity) return "-Infinity";
            }
            return JSON.stringify(arg);
          }
          return String(arg);
        });
        const before = cases.length;
        try {
          swallowAsync(fn(...args));
        } catch (e: any) {
          const prefix = describeStack.length > 0 ? describeStack.join(" ") + " " : "";
          if (cases.length === before) {
            extractionErrors.push({ name: prefix + name, error: e.message ?? String(e) });
          }
        }
        const prefix = describeStack.length > 0 ? describeStack.join(" ") + " " : "";
        for (let i = before; i < cases.length; i++) {
          cases[i].name = prefix + name;
        }
      }
    };
  };

  // test.skip: create a no-op that mirrors testFn's shape but doesn't capture cases
  // oxlint-disable-next-line unicorn/consistent-function-scoping
  const skipFn: any = () => {};
  skipFn.each = () => () => {};
  testFn.skip = skipFn;
  testFn.only = testFn;

  const describeFn: any = (name: string, fn: () => void) => {
    describeStack.push(name);
    const before = cases.length;
    try {
      swallowAsync(fn());
    } catch (e: any) {
      if (cases.length === before) {
        extractionErrors.push({ name: describeStack.join(" "), error: e.message ?? String(e) });
      }
    }
    describeStack.pop();
  };
  describeFn.each = testFn.each;
  describeFn.skip = skipFn;

  const specSource = fs.readFileSync(specPath, "utf-8");
  const jsCode = ts.transpileModule(specSource, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
  }).outputText;

  const exports: any = {};
  const context = vm.createContext({
    exports,
    module: { exports },
    Object,
    require: (mod: string) => {
      if (mod.endsWith("/util") || mod.endsWith("\\util")) {
        return {
          ...util,
          // ExecutionError class for tests that check runtime errors
          ExecutionError: class ExecutionError extends Error {
            name = "ExecutionError";
          },
          // TestBuilder type — only used for TS type annotations, not runtime
          TestBuilder: () => {},
          assert: () => {},
        };
      }
      if (mod === "typescript") {
        return ts;
      }
      if (mod === "path") {
        return path;
      }
      if (mod === "assert") {
        return { strict: () => {} };
      }
      if (mod.includes("find-lua-requires")) {
        return {
          findLuaRequires: () =>
            Array.from({ length: 10 }, () => ({ requirePath: "", from: 0, to: 0 })),
        };
      }
      if (mod.includes("safe-names")) {
        return {
          luaKeywords: new Set([
            "and",
            "bit",
            "bit32",
            "break",
            "do",
            "else",
            "elseif",
            "end",
            "false",
            "for",
            "function",
            "goto",
            "if",
            "in",
            "local",
            "nil",
            "not",
            "or",
            "repeat",
            "return",
            "then",
            "true",
            "until",
            "while",
          ]),
        };
      }
      if (mod.includes("diagnostics")) {
        // Codes match the TSTL test environment (where src entry point is imported
        // first, shifting all codes by 1 from standalone values).
        const knownDiags: Record<string, any> = {
          unsupportedNodeKind: diagFactory(100013),
          forbiddenForIn: diagFactory(100014),
          unsupportedNoSelfFunctionConversion: diagFactory(100015),
          unsupportedSelfFunctionConversion: diagFactory(100016),
          unsupportedOverloadAssignment: diagFactory(100017),
          decoratorInvalidContext: diagFactory(100018),
          annotationInvalidArgumentCount: diagFactory(100019),
          invalidRangeUse: diagFactory(100020),
          invalidVarargUse: diagFactory(100021),
          invalidRangeControlVariable: diagFactory(100022),
          invalidMultiIterableWithoutDestructuring: diagFactory(100023),
          invalidPairsIterableWithoutDestructuring: diagFactory(100024),
          unsupportedAccessorInObjectLiteral: diagFactory(100025),
          unsupportedRightShiftOperator: diagFactory(100026),
          unsupportedForTarget: diagFactory(100027),
          unsupportedForTargetButOverrideAvailable: diagFactory(100028),
          unsupportedProperty: diagFactory(100029),
          invalidAmbientIdentifierName: diagFactory(100030),
          unsupportedVarDeclaration: diagFactory(100031),
          invalidMultiFunctionUse: diagFactory(100032),
          invalidMultiFunctionReturnType: diagFactory(100033),
          invalidMultiReturnAccess: diagFactory(100034),
          invalidCallExtensionUse: diagFactory(100035),
          annotationDeprecated: diagFactory(100036),
          truthyOnlyConditionalValue: diagFactory(100037),
          notAllowedOptionalAssignment: diagFactory(100038),
          awaitMustBeInAsyncFunction: diagFactory(100039),
          unsupportedBuiltinOptionalCall: diagFactory(100040),
          unsupportedOptionalCompileMembersOnly: diagFactory(100041),
          undefinedInArrayLiteral: diagFactory(100042),
          invalidMethodCallExtensionUse: diagFactory(100043),
          invalidSpreadInCallExtension: diagFactory(100044),
          cannotAssignToNodeOfKind: diagFactory(100045),
          incompleteFieldDecoratorWarning: diagFactory(100046),
          unsupportedArrayWithLengthConstructor: diagFactory(100047),
          couldNotResolveRequire: diagFactory(100048),
        };
        // Proxy fallback for unknown diagnostics (e.g. transpilation diagnostics)
        return new Proxy(knownDiags, {
          get(target, prop: string | symbol) {
            if (typeof prop === "symbol") return undefined;
            if (prop in target) return target[prop];
            return diagFactory(0);
          },
        });
      }
      if (mod.includes("tstl") || mod.includes("/src") || mod === "../src" || mod === "../../src") {
        // Unified mock for all TSTL internal imports (tstl, src/*, diagnostics from src, etc.)
        // Use a deep Proxy: unknown properties return a diagFactory-like object that also
        // supports property access (for enums like BuildMode.Library, JsxEmit.React, etc.)
        const tstlMock: Record<string, any> = {
          LuaTarget: {
            Universal: "universal",
            Lua50: "Lua 5.0",
            Lua51: "Lua 5.1",
            Lua52: "Lua 5.2",
            Lua53: "Lua 5.3",
            Lua54: "Lua 5.4",
            Lua55: "Lua 5.5",
            LuaJIT: "LuaJIT",
            Luau: "Luau",
          },
          LuaLibImportKind: {
            None: "none",
            Require: "require",
            Inline: "inline",
          },
          transpileString: () => ({ diagnostics: [], file: { lua: "" } }),
          transpileVirtualProject: () => ({ diagnostics: [], transpiledFiles: [] }),
        };
        return new Proxy(tstlMock, {
          get(target, prop: string | symbol) {
            if (typeof prop === "symbol") return undefined;
            if (prop in target) return target[prop];
            // Return a diagFactory that also supports property access (for enums).
            // diagFactory(code) returns a callable with .code; we wrap it in a Proxy
            // so .AnyProp also works (returns a string enum-like value).
            const factory = diagFactory(0);
            return new Proxy(factory, {
              get(ft: any, p: string) {
                if (p in ft) return ft[p];
                if (typeof p === "symbol") return undefined;
                return p; // enum-like: BuildMode.Library → "Library"
              },
            });
          },
        });
      }
      // Try resolving as a relative .ts file next to the spec
      if (mod.startsWith("./") || mod.startsWith("../")) {
        const specDir = path.dirname(specPath);
        const resolved = path.resolve(specDir, mod) + ".ts";
        if (fs.existsSync(resolved)) {
          try {
            const source = fs.readFileSync(resolved, "utf-8");
            const compiled = ts.transpileModule(source, {
              compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
            }).outputText;
            const modExports: any = {};
            const modModule = { exports: modExports };
            vm.runInContext(
              `(function(exports, module) { ${compiled} })(exports, module)`,
              vm.createContext({ exports: modExports, module: modModule, Object, console }),
            );
            return modModule.exports;
          } catch {}
        }
      }
      return {};
    },
    test: testFn,
    describe: describeFn,
    expect: Object.assign(
      (value: unknown) => {
        // Route codegen assertions from getMainLuaCodeChunk() back to the test case
        if (value != null && typeof value === "object" && (value as any)[LUA_CODE_BRAND]) {
          const tc = (value as any)[LUA_CODE_BRAND] as TestCase;
          const capture = (arr: "contains" | "notContains") => (s: string) => {
            if (!tc.codegen) tc.codegen = {};
            if (!tc.codegen[arr]) tc.codegen[arr] = [];
            tc.codegen[arr]!.push(s);
          };
          const captureMatch = (arr: "matches" | "notMatches") => (s: string | RegExp) => {
            // Use duck-typing instead of instanceof — RegExp from VM context
            // has a different constructor than the host's RegExp.
            if (typeof s === "object" && s !== null && "source" in s && "flags" in s) {
              if (!tc.codegen) tc.codegen = {};
              if (!tc.codegen[arr]) tc.codegen[arr] = [];
              tc.codegen[arr]!.push((s as RegExp).source);
            } else if (typeof s === "string") {
              capture(arr === "matches" ? "contains" : "notContains")(s);
            }
          };
          return {
            toContain: capture("contains"),
            toMatch: captureMatch("matches"),
            toBe: () => {},
            not: { toContain: capture("notContains"), toMatch: captureMatch("notMatches") },
          };
        }
        return noopMatcherProxy();
      },
      {
        stringContaining: (s: string) => stringContaining(s),
        arrayContaining: (a: unknown[]) => a,
        objectContaining: (o: unknown) => o,
        anything: () => undefined,
        any: (c: unknown) => c,
      },
    ),
    __dirname: path.dirname(specPath),
    __filename: specPath,
    path,
    JSON,
    Buffer,
    console,
  });
  sandboxRef.current = context;

  try {
    vm.runInContext(jsCode, context);
  } catch (e: any) {
    extractionErrors.push({ name: "(top-level)", error: e.message ?? String(e) });
  }
  return { cases, extractionErrors };
}
