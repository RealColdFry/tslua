import { tstlDiagCode } from "./tstl-diagnostics.ts";

export const diagFactory = (code: number) => Object.assign(() => ({}), { code });

// ---- TSTL bug overrides ----
// When TSTL's test has a wrong expected value (verified against JS runtime),
// override it here. Key format: "specBaseName::testName".
// When TSTL's expected value is wrong, override it here with the correct one.
class ExecutionError extends Error {
  override name = "ExecutionError";
}

export const tstlBugOverrides: Record<string, { expectedValue: unknown; bug: string }> = {
  // Baking limitation: ts.transpileModule doesn't inline declare const enums
  // (needs full program.emit()). The correct value is {A: 0} since const enums inline.
  // If we hit more declare-related baking failures, switch evaluateJS from
  // ts.transpileModule to ts.createProgram + program.emit() (what TSTL uses).
  "enum::const enum declare without initializer": {
    expectedValue: { A: 0 },
    bug: "bake-limitation: declare const enum needs program.emit()",
  },
  // Baking limitation: JS says "stack overflow", Lua says "C stack overflow"
  "bundle::cyclic imports": {
    expectedValue: new ExecutionError("C stack overflow"),
    bug: "bake-limitation: Lua error message differs from JS",
  },
};

// Baking limitation overrides: tests where the expected value can't be computed by
// JS baking alone (e.g., needs Lua execution for platform-specific strings).
// Matched by "specBaseName::testName" AND tsCode substring.
export const bakeLimitationOverrides: {
  key: string;
  codeContains: string;
  expectedValue: unknown;
  bug: string;
}[] = [
  // getLuaExecutionResult() needed to get Lua's tostring() for NaN/Infinity/-Infinity.
  // Lua consistently uses "nan", "inf", "-inf" across all versions.
  {
    key: "array::array.length set throws on invalid special values (NaN)",
    codeContains: "NaN",
    expectedValue: new ExecutionError("invalid array length: nan"),
    bug: "bake-limitation: getLuaExecutionResult for NaN",
  },
  {
    key: "array::array.length set throws on invalid special values (Infinity)",
    codeContains: "= Infinity",
    expectedValue: new ExecutionError("invalid array length: inf"),
    bug: "bake-limitation: getLuaExecutionResult for Infinity",
  },
  {
    key: "array::array.length set throws on invalid special values (-Infinity)",
    codeContains: "-Infinity",
    expectedValue: new ExecutionError("invalid array length: -inf"),
    bug: "bake-limitation: getLuaExecutionResult for -Infinity",
  },
];

// Skip entire test (eval + codegen). Use when the test can't pass on native Lua
// due to runtime differences (e.g., error(nil) behavior).
export const tstlBugSkips: Record<string, string> = {
  "error::throw and catch undefined":
    "TSTL bug: error(nil) behaves differently on native Lua vs JS",
  // tsgo doesn't parse JSDoc inside namespace blocks, so @customName on inner members is lost
  "identifiers::customName rename namespace": "tsgo-limitation: JSDoc in namespace blocks",
  // tsgo errors on legacy `module` keyword for namespaces
  "namespaces::legacy internal module syntax": "tsgo-limitation: module keyword for namespaces",
  // tslua supports labeled statements; TSTL does not
  "statements::Unsupported node adds diagnostic": "tslua-feature: labeled statements supported",
  // Luau: lualib async/coroutine bundle uses patterns incompatible with Luau runtime
  "async-await::await inside try/catch returns inside async function [Luau]":
    "luau: lualib async bundle incompatible with Luau coroutine runtime",
  "async-await::await inside try/catch throws inside async function [Luau]":
    "luau: lualib async bundle incompatible with Luau coroutine runtime",
  "async-await::await inside try/catch deferred rejection uses catch clause [Luau]":
    "luau: lualib async bundle incompatible with Luau coroutine runtime",
  // Lune's `_G` is empty (Luau builtins only reachable as barewords), so the
  // lualib `local ____pcall = _G.pcall` returns nil and __TS__Promise errors
  // on construction. See upstream TSTL lualib patch proposal.
  "async-await::break inside try in async loop (#1706) [Luau]":
    "luau: lualib uses _G.pcall which is nil in Lune",
  "async-await::continue inside try in async loop (#1706) [Luau]":
    "luau: lualib uses _G.pcall which is nil in Lune",
};

// Skip codegen comparison only (eval still runs). Use when tslua's codegen is
// correct but differs from TSTL due to a TSTL bug in code generation.
export const tstlBugCodegenSkips: Record<string, string> = {
  // tslua uses `local Foo = {}` instead of `Foo = Foo or ({})` for same-file enum merging,
  // avoiding the global scope leak that TSTL's pattern causes.
  "enum::enum nested in namespace":
    "tslua-improvement: same-file namespace enum uses plain {} init",
  // tslua emits 0xFFFFFFFF (hex) for the unsigned right shift mask; TSTL emits 4294967295 (decimal).
  // Hex is more readable for a bitmask. Same value, different literal format.
  'expressions::Bitop [5.3] ("a>>>b")': "tslua-improvement: hex mask literal",
  'expressions::Bitop [5.3] ("a>>>=b")': "tslua-improvement: hex mask literal",
  'expressions::Bitop [5.4] ("a>>>b")': "tslua-improvement: hex mask literal",
  'expressions::Bitop [5.4] ("a>>>=b")': "tslua-improvement: hex mask literal",
  'expressions::Bitop [5.5] ("a>>>b")': "tslua-improvement: hex mask literal",
  'expressions::Bitop [5.5] ("a>>>=b")': "tslua-improvement: hex mask literal",
  // Luau: tslua duplicates for-loop incrementor before continue to avoid infinite loop.
  // TSTL emits bare continue which skips the incrementor — a bug.
  "loops::for with continue [Luau]":
    "tslua-improvement: for-loop continue duplicates incrementor (TSTL bug: infinite loop)",
  // Pre-existing spread/vararg codegen diffs (not Luau-specific)
  "spread::finally clause [Luau]": "tslua-improvement: vararg spread handling differs from TSTL",
  "spread::self-referencing function expression [Luau]":
    "tslua-improvement: vararg spread handling differs from TSTL",
  "spread::tagged template method indirect access [Luau]":
    "tslua-improvement: vararg spread handling differs from TSTL",
};

// Skip codegen snapshot in batchExpectDiagnostics. Use when tslua's codegen is
// correct but intentionally differs from TSTL (e.g., optimizations, unimplemented features).
export const diagSnapshotSkips: Set<string> = new Set([
  // tslua optimizes away ____opt_0 temp for simple identifiers in optional chains
  "optionalChaining::Unsupported optional chains Compile members only",
  "optionalChaining::Unsupported optional chains Builtin global property",
  "optionalChaining::Unsupported optional chains Builtin global method",
  "optionalChaining::Unsupported optional chains Builtin prototype method",
  "optionalChaining::Unsupported optional chains Language extensions",
  // tslua doesn't emit @customConstructor annotation comments
  "customConstructor::IncorrectUsage",
  // tslua doesn't implement sourceMapTraceback
  "error::sourceMapTraceback maps anonymous function locations in .lua files (#1665)",
  // tslua uses TC39 decorators; legacy experimentalDecorators not yet distinguished in batch tests
  "decorators::legacy experimentalDecorators Throws error if decorator function has void context",
  // tslua optimizes vararg spread in constructors differently
  "classes::vararg spread optimization in class constructor (#1673)",
  // tslua optimizes $vararg spread differently
  'vararg::$vararg invalid use ("function foo(...args: string[]) {} function bar() { foo(...$vararg); }")',
  // tslua's multi destructuring wrapping differs from TSTL
  "multi::invalid direct $multi function use (let a; [a] = $multi())",
  "multi::invalid $multi call (([a] = $multi(1)) => {})",
  "multi::invalid direct $multi function use (let a; for ([a] = $multi(1, 2); false; 1) {})",
  "multi::invalid direct $multi function use (let a; for (const [a] = $multi(1, 2); false; 1) {})",
  // tslua uses table wrapping for non-numeric access differently
  "multi::disallow LuaMultiReturn non-numeric access",
  // tslua emits different diagnostic code for shorthand property with $multi
  "multi::invalid $multi call (({ $multi });)",
]);

// Skip codegen contains/notContains/matches/notMatches assertions in batchExpectCodegen.
// Use when tslua's codegen intentionally differs from TSTL (optimizations, missing features).
export const codegenAssertionSkips: Set<string> = new Set([
  // tslua doesn't implement sourceMapTraceback
  "error::sourceMapTraceback maps anonymous function locations in .lua files (#1665)",
  // tslua doesn't yet optimize vararg spread in constructors the same way
  "classes::vararg spread optimization in class constructor (#1673)",
]);

// Skip specific diagnostic codes that tslua emits differently.
// Key: "specBase::testName", value: set of TSTL diagnostic factory names to skip.
const diagCodeSkipsByName: Map<string, Set<string>> = new Map([
  // tslua emits invalidAmbientIdentifierName before invalidMultiFunctionUse
  ["multi::invalid $multi call (({ $multi });)", new Set(["invalidMultiFunctionUse"])],
]);

export const diagCodeSkips: Map<string, Set<number>> = new Map(
  Array.from(diagCodeSkipsByName, ([k, names]) => [k, new Set(Array.from(names, tstlDiagCode))]),
);

// Force allowDiagnostics on specific tests where our batching strategy causes
// diagnostic conflicts that don't exist in TSTL (which compiles each test independently).
// Key: "specBase::testName".
export const batchDiagnosticOverrides: Set<string> = new Set([
  // Batch has declare global { var foo: string } and { var foo: () => string } — conflicts when compiled together.
  "globalThis::function call",
]);

// Supported targets for migration (must match transpiler.LuaTarget constants)
export const supportedTargets = [
  "JIT",
  "5.0",
  "5.1",
  "5.2",
  "5.3",
  "5.4",
  "5.5",
  "Luau",
  "universal",
] as const;

export const supportedTargetValues: Set<string> = new Set(supportedTargets);

// Map from TSTL LuaTarget enum values to our target strings
export const tstlTargetMap: Record<string, string> = {
  universal: "universal",
  "Lua 5.0": "5.0",
  "Lua 5.1": "5.1",
  "Lua 5.2": "5.2",
  "Lua 5.3": "5.3",
  "Lua 5.4": "5.4",
  "Lua 5.5": "5.5",
  LuaJIT: "JIT",
  Luau: "Luau",
};

// Map from target string to Go constant name
export const goTargetConst: Record<string, string> = {
  JIT: "transpiler.LuaTargetLuaJIT",
  "5.0": "transpiler.LuaTargetLua50",
  "5.1": "transpiler.LuaTargetLua51",
  "5.2": "transpiler.LuaTargetLua52",
  "5.3": "transpiler.LuaTargetLua53",
  "5.4": "transpiler.LuaTargetLua54",
  "5.5": "transpiler.LuaTargetLua55",
  Luau: "transpiler.LuaTargetLuau",
  universal: "transpiler.LuaTargetUniversal",
};

// Reverse map: TSTL enum display values ("LuaJIT", "Lua 5.2") -> our target strings ("JIT", "5.2")
export const tstlEnumToTarget: Record<string, string> = {
  universal: "universal",
  "Lua 5.0": "5.0",
  "Lua 5.1": "5.1",
  "Lua 5.2": "5.2",
  "Lua 5.3": "5.3",
  "Lua 5.4": "5.4",
  "Lua 5.5": "5.5",
  LuaJIT: "JIT",
  Luau: "Luau",
};

// TSTL diagnostic factories that tslua implements. Codes resolve from the real
// TSTL modules so they track upstream changes automatically. Codes not in this
// set are filtered out of migrated tests.
const supportedTstlDiagNames: readonly string[] = [
  "unsupportedNodeKind",
  "forbiddenForIn",
  "unsupportedNoSelfFunctionConversion",
  "unsupportedSelfFunctionConversion",
  "unsupportedOverloadAssignment",
  "invalidRangeUse",
  "invalidVarargUse",
  "invalidRangeControlVariable",
  "invalidMultiIterableWithoutDestructuring",
  "invalidPairsIterableWithoutDestructuring",
  "unsupportedRightShiftOperator",
  "unsupportedForTarget",
  "unsupportedForTargetButOverrideAvailable",
  "unsupportedProperty",
  "invalidAmbientIdentifierName",
  "unsupportedVarDeclaration",
  "invalidMultiFunctionUse",
  "invalidMultiFunctionReturnType",
  "invalidMultiReturnAccess",
  "invalidCallExtensionUse",
  "awaitMustBeInAsyncFunction",
  "undefinedInArrayLiteral",
  "invalidMethodCallExtensionUse",
  "invalidSpreadInCallExtension",
  "unsupportedArrayWithLengthConstructor",
];

export const supportedTstlDiagCodes: Set<number> = new Set(
  supportedTstlDiagNames.map(tstlDiagCode),
);
