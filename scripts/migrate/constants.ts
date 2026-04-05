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
// Key: "specBase::testName", value: set of codes to skip.
export const diagCodeSkips: Map<string, Set<number>> = new Map([
  // tslua emits InvalidAmbientIdentifierName (100030) before InvalidMultiFunctionUse (100032)
  ["multi::invalid $multi call (({ $multi });)", new Set([100032])],
]);

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
};

// TSTL diagnostic codes used in the test environment (where src entry point is imported first).
// tslua uses these same codes directly, so no mapping is needed.
// Codes that are not in this set are not yet implemented in tslua and are filtered out.
export const supportedTstlDiagCodes: Set<number> = new Set([
  100013, // unsupportedNodeKind
  100014, // forbiddenForIn
  100015, // unsupportedNoSelfFunctionConversion
  100016, // unsupportedSelfFunctionConversion
  100017, // unsupportedOverloadAssignment
  100020, // invalidRangeUse
  100021, // invalidVarargUse
  100022, // invalidRangeControlVariable
  100023, // invalidMultiIterableWithoutDestructuring
  100024, // invalidPairsIterableWithoutDestructuring
  100026, // unsupportedRightShiftOperator
  100027, // unsupportedForTarget
  100028, // unsupportedForTargetButOverrideAvailable
  100029, // unsupportedProperty
  100030, // invalidAmbientIdentifierName
  100031, // unsupportedVarDeclaration
  100032, // invalidMultiFunctionUse
  100033, // invalidMultiFunctionReturnType
  100034, // invalidMultiReturnAccess
  100035, // invalidCallExtensionUse
  100039, // awaitMustBeInAsyncFunction
  100042, // undefinedInArrayLiteral
  100043, // invalidMethodCallExtensionUse
  100044, // invalidSpreadInCallExtension
  100047, // unsupportedArrayWithLengthConstructor
]);
