import type { Mode, TestCase, SandboxRef } from "./types.ts";

export const INERT_PROXY_BRAND = Symbol.for("tslua.inertProxy");
export const LUA_CODE_BRAND = Symbol.for("tslua.luaCode");

export function isInertProxy(v: unknown): boolean {
  return (
    v != null &&
    (typeof v === "object" || typeof v === "function") &&
    (v as any)[INERT_PROXY_BRAND] === true
  );
}

export function stringifyValue(v: unknown): string {
  if (v === undefined) return "undefined";
  if (v === null) return "null";
  if (typeof v === "number") {
    if (Number.isNaN(v)) return "NaN";
    if (v === Infinity) return "Infinity";
    if (v === -Infinity) return "-Infinity";
    return String(v);
  }
  if (typeof v === "string") return JSON.stringify(v);
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

export function createBuilder(mode: Mode, code: string, cases: TestCase[], sandboxRef: SandboxRef) {
  const builder: any = {};
  const self = () => builder;

  // Accumulated state
  let extraFiles: Record<string, string> = {};
  let returnExport: string[] | undefined;
  let options: Record<string, unknown> = {};
  let tsHeader = "";
  let luaHeader = "";
  let langExt = false;
  let entryPoint = "";
  let mainFileName = "";
  let allowDiagnostics = false;

  function pushCase(
    assertion: TestCase["assertion"],
    expectedValue?: unknown,
    otherReason?: string,
  ): TestCase {
    const tc: TestCase = { name: "", mode, tsCode: code, assertion };
    if (langExt) tc.languageExtensions = true;
    if (tsHeader) tc.tsHeader = tsHeader;
    if (luaHeader) tc.luaHeader = luaHeader;
    if (expectedValue !== undefined) tc.expectedValue = expectedValue;
    if (Object.keys(extraFiles).length > 0) tc.extraFiles = { ...extraFiles };
    if (returnExport) tc.returnExport = [...returnExport];
    if (Object.keys(options).length > 0) tc.options = { ...options };
    if (
      codegenContains.length > 0 ||
      codegenNotContains.length > 0 ||
      codegenMatches.length > 0 ||
      codegenNotMatches.length > 0
    ) {
      tc.codegen = {};
      if (codegenContains.length > 0) tc.codegen.contains = [...codegenContains];
      if (codegenNotContains.length > 0) tc.codegen.notContains = [...codegenNotContains];
      if (codegenMatches.length > 0) tc.codegen.matches = [...codegenMatches];
      if (codegenNotMatches.length > 0) tc.codegen.notMatches = [...codegenNotMatches];
    }
    if (luaFactory) tc.luaFactory = luaFactory;
    if (entryPoint) tc.entryPoint = entryPoint;
    if (mainFileName) tc.mainFileName = mainFileName;
    if (allowDiagnostics) tc.allowDiagnostics = true;
    if (otherReason) tc.otherReason = otherReason;
    cases.push(tc);
    return tc;
  }

  builder.expectToMatchJsResult = (allowErrors?: boolean) => {
    const tc = pushCase("matchJsResult");
    if (allowErrors) tc.allowErrors = true;
    return builder;
  };
  builder.expectToEqual = (expected: unknown) => {
    pushCase("equal", expected);
    return builder;
  };
  builder.expectLuaToMatchSnapshot = () => {
    // Will be resolved to codegen assertion after test names are assigned
    pushCase("snapshot");
    return builder;
  };
  builder.expectDiagnosticsToMatchSnapshot = (codes?: number[]) => {
    const tc: TestCase = { name: "", mode, tsCode: code, assertion: "diagnostic" };
    if (codes && codes.length > 0) tc.expectedDiagCodes = codes;
    if (langExt) tc.languageExtensions = true;
    if (tsHeader) tc.tsHeader = tsHeader;
    if (Object.keys(extraFiles).length > 0) tc.extraFiles = { ...extraFiles };
    if (returnExport) tc.returnExport = [...returnExport];
    if (Object.keys(options).length > 0) tc.options = { ...options };
    if (mainFileName) tc.mainFileName = mainFileName;
    cases.push(tc);
    return builder;
  };
  builder.expectToHaveDiagnostics = builder.expectDiagnosticsToMatchSnapshot;
  builder.expectToHaveNoDiagnostics = self;
  builder.expectNoExecutionError = self;

  // Stateful config methods
  builder.setOptions = (opts: Record<string, unknown>) => {
    Object.assign(options, opts);
    // Detect language extensions from types array (e.g. operatorsProjectOptions)
    if (
      Array.isArray(opts.types) &&
      opts.types.some((t: unknown) => typeof t === "string" && t.includes("language-extensions"))
    ) {
      langExt = true;
    }
    return builder;
  };
  builder.addExtraFile = (name: string, fileCode: string) => {
    extraFiles[name] = fileCode;
    return builder;
  };
  builder.setReturnExport = (...names: string[]) => {
    returnExport = names;
    return builder;
  };

  builder.setTsHeader = (header: string) => {
    tsHeader = header;
    return builder;
  };

  builder.setLuaHeader = (header: string) => {
    // Strip 'local' from variable declarations so they become globals.
    // Our test runner uses require() which isolates module scope, so
    // header locals aren't visible to the module. TSTL's WASM runner
    // doesn't have this isolation.
    luaHeader = header.replace(/\blocal\s+(\w+\s*=)/g, "$1");
    return builder;
  };

  builder.withLanguageExtensions = () => {
    langExt = true;
    return builder;
  };

  let luaFactory = "";
  builder.setLuaFactory = (factory: (code: string) => string) => {
    // Evaluate the factory with a placeholder to extract the wrapping pattern
    const result = factory("__CODE__");
    // Convert to Go: the factory wraps code in a function call
    luaFactory =
      `func(code string) string { return ` +
      JSON.stringify(result).replace("__CODE__", `" + code + "`) +
      ` }`;
    return builder;
  };

  builder.setEntryPoint = (ep: string) => {
    entryPoint = ep;
    return builder;
  };

  builder.setMainFileName = (name: string) => {
    mainFileName = name;
    return builder;
  };

  builder.ignoreDiagnostics = (_codes?: number[]) => {
    allowDiagnostics = true;
    return builder;
  };
  builder.disableSemanticCheck = () => {
    allowDiagnostics = true;
    return builder;
  };

  // No-op config methods (return builder for chaining)
  for (const m of ["setJsHeader", "setCustomTransformers", "expectNoTranspileException", "debug"]) {
    builder[m] = self;
  }

  // Terminal methods — return inert values so test bodies that inspect results
  // (via expect()) don't throw. The mock expect is a no-op anyway.
  // Use Proxy for execution results so arbitrary property access (e.g. .message,
  // .code, .split(), [0]) returns something safe instead of throwing.
  const inertProxy: any = new Proxy(() => inertProxy, {
    get(_target, prop) {
      if (prop === INERT_PROXY_BRAND) return true;
      if (prop === Symbol.iterator) return undefined;
      if (prop === Symbol.toPrimitive) return () => "";
      if (prop === "then") return undefined; // not a Promise
      return inertProxy;
    },
  });
  // Terminal methods push an "other" assertion (filtered out during migration)
  // so the test is counted but doesn't produce a bogus Go test case.
  // Return inert values so test bodies that access the result don't throw.
  builder.getMainLuaCodeChunk = () => {
    const tc = pushCase("other", undefined, "getMainLuaCodeChunk");
    // Return a branded string so expect() in the sandbox can route
    // .toContain/.toMatch assertions back to this case's codegen field.
    const branded: any = new String("");
    branded[LUA_CODE_BRAND] = tc;
    return branded;
  };
  builder.getLuaExecutionResult = () => {
    pushCase("other", undefined, "getLuaExecutionResult");
    return inertProxy;
  };
  builder.getJsExecutionResult = () => {
    pushCase("other", undefined, "getJsExecutionResult");
    return inertProxy;
  };
  builder.getLuaResult = () => {
    pushCase("other", undefined, "getLuaResult");
    return {
      diagnostics: [
        {
          code: 0,
          messageText: "",
          category: 0,
          file: undefined,
          start: undefined,
          length: undefined,
        },
      ],
      transpiledFiles: [{ lua: "", luaSourceMap: "{}", outPath: "main.lua", sourceFiles: [] }],
    };
  };
  builder.getMainLuaFileResult = () => {
    pushCase("other", undefined, "getMainLuaFileResult");
    return { lua: "", luaSourceMap: "{}", outPath: "main.lua", sourceFiles: [] };
  };
  // Accumulated codegen assertions from .tap() calls
  let codegenContains: string[] = [];
  let codegenNotContains: string[] = [];
  let codegenMatches: string[] = [];
  let codegenNotMatches: string[] = [];

  builder.tap = (fn: (builder: any) => void) => {
    // Extract assertions by running fn with a mock builder+expect that captures calls
    const mockExpect = (_value: unknown) => {
      const matchers: any = {
        toContain: (s: string) => {
          codegenContains.push(s);
        },
        toMatch: (s: string | RegExp) => {
          // Duck-type RegExp — instanceof fails across VM context boundaries
          if (typeof s === "object" && s !== null && "source" in s && "flags" in s) {
            codegenMatches.push((s as RegExp).source);
          } else if (typeof s === "string") {
            codegenContains.push(s);
          }
        },
        not: {
          toContain: (s: string) => {
            codegenNotContains.push(s);
          },
          toMatch: (s: string | RegExp) => {
            if (typeof s === "object" && s !== null && "source" in s && "flags" in s) {
              codegenNotMatches.push((s as RegExp).source);
            } else if (typeof s === "string") {
              codegenNotContains.push(s);
            }
          },
        },
      };
      return matchers;
    };
    const mockBuilder = { getMainLuaCodeChunk: () => "<<LUA_CODE>>" };
    try {
      // fn is from the VM sandbox — rebind expect in the sandbox context before calling
      const prevExpect = sandboxRef.current?.expect;
      if (sandboxRef.current) sandboxRef.current.expect = mockExpect;
      fn(mockBuilder);
      if (sandboxRef.current) sandboxRef.current.expect = prevExpect;
    } catch {}
    return builder;
  };
  return builder;
}

export function taggedTemplate(
  mode: Mode,
  cases: TestCase[],
  sandboxRef: SandboxRef,
  serializeSubstitutions = false,
) {
  return (strings: TemplateStringsArray | string, ...values: unknown[]) => {
    let code: string;
    if (typeof strings === "string") {
      code = strings;
    } else {
      const subs = serializeSubstitutions ? values.map(stringifyValue) : values.map(String);
      code = strings[0];
      for (let i = 0; i < subs.length; i++) {
        code += subs[i] + strings[i + 1];
      }
    }
    return createBuilder(mode, code.trim(), cases, sandboxRef);
  };
}
