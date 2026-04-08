---
title: Testing
description: How tslua verifies compatibility with TypeScriptToLua.
---

tslua uses three complementary testing approaches to verify compatibility with TSTL.

## Jest harness

TSTL's own Jest test suite runs unmodified, but with tslua's transpiler swapped in. This is the most authoritative compatibility check since it uses TSTL's own test infrastructure and assertions.

### How it works

A patch (`extern/tstl-test-util.patch`) modifies TSTL's `test/util.ts` to check for a `TSTL_GO=1` environment variable. When set, the test builder sends transpilation requests to a tslua server instead of calling TSTL's transpiler. TSTL's existing Lua WASM runtime then evaluates the output and checks assertions as normal.

### Server and client

`tslua server --socket PATH` starts a long-lived process listening on a Unix socket. Jest workers send requests through `tslua-client`, a minimal Go binary that connects to the socket, pipes a JSON request from stdin, and writes the response to stdout.

**Request** (JSON):

```json
{
  "source": "const x: number = 1; print(x);",
  "luaTarget": "5.4",
  "mainFileName": "main.ts",
  "extraFiles": { "helper.ts": "export const y = 2;" },
  "compilerOptions": { "strict": true, "target": "ESNext" }
}
```

**Response** (JSON):

```json
{
  "ok": true,
  "files": { "main.lua": "x = 1\nprint(x)" },
  "diagnostics": []
}
```

The server processes requests sequentially through a single worker goroutine with panic recovery. This avoids concurrency issues with the typescript-go type checker while keeping the server alive across requests. Each request has a 10-second timeout.

```bash
just tstl-test  # run full suite
```

```bash
just tstl-test expressions  # filter by spec name
```

Current result: **6071 / 6179 tests pass (98.3%).** The 103 failures break down as:

| Category               | Count | Notes                                                                                                                  |
| ---------------------- | ----- | ---------------------------------------------------------------------------------------------------------------------- |
| Codegen snapshot diffs | ~23   | Semantically equivalent output, different formatting (e.g. hex vs decimal literals, temp variable elision)             |
| Plugins/transformers   | ~14   | `luaPlugins` not implemented                                                                                           |
| Bundling/build modes   | ~11   | `--buildMode library`, `luaLibImport inline`, bundling edge cases                                                      |
| Module resolution      | ~8    | `noResolvePaths`, `baseUrl` resolution differences                                                                     |
| Emit paths             | ~4    | `getEmitPath` for outDir/rootDir/extensions                                                                            |
| Diagnostics            | ~2    | Snapshot diffs in diagnostic output                                                                                    |
| Other                  | ~41   | Mix of Luau-specific, language extension edge cases, async/generators on universal target, declaration file generation |

Most failures are in unimplemented features (plugins, build modes) or codegen snapshot comparisons where the output is semantically equivalent but not byte-identical.

## Migrated Go tests

A [migration system](https://github.com/RealColdFry/tslua/tree/master/scripts/migrate) extracts TSTL's Jest specs and code-generates them into native Go tests under `internal/tstltest/`. This gives us fast, reproducible test runs without needing Node or TSTL installed.

The migration script (`scripts/migrate/cli.ts`) works by running each TSTL spec file in a sandboxed VM with mock `util.testExpression` / `util.testFunction` / `util.testModule` builders. Instead of executing the tests, these builders capture the test structure (TypeScript source, options, expected values) and emit Go test files.

For each TSTL test case, two Go tests are generated:

- **`TestEval_*`** - transpiles the TypeScript with tslua, runs the Lua output, and checks it against the expected value (either baked from JS evaluation or specified inline). These are the behavioral correctness tests.
- **`TestCodegen_*`** - transpiles with both tslua and TSTL, then diffs the Lua output. These catch formatting and structural divergences but are not required to pass (`just testall` skips them).

```bash
just migrate expressions    # migrate a specific TSTL spec
just migrate-all            # regenerate all migrated test files
node --require tsx/cjs scripts/migrate/cli.ts -c -a  # check migration coverage
```

Current result: **5656 / 5903 cases migrated (95.8%)** across 70 of 71 spec files, with **100% behavioral pass rate** on migrated cases.

The 1 unmigrated spec file (`find-lua-requires`) tests a TSTL-internal Lua source parser that scans emitted Lua for `require()` calls (used by TSTL's bundler). tslua doesn't need this: it tracks dependencies at the TypeScript AST level during transpilation and uses TypeScript's own module resolution, so there's no post-emission Lua scanning. The spec also uses plain Jest assertions with no `testExpression`/`testFunction`/`testModule` builders, so the migration system has nothing to capture.

The 247 unmigrated cases within migrated specs use TSTL assertion methods (`getMainLuaCodeChunk`, `getLuaExecutionResult`, etc.) that the migration script doesn't yet support. These are captured and reported by the `-c` (check) flag.

### Overrides

Not every TSTL test can be migrated as-is. `scripts/migrate/constants.ts` defines several override categories for cases that need special handling:

- **`tstlBugOverrides`** - TSTL's expected value is wrong (verified against JS runtime). The migration uses the corrected value instead.
- **`tstlBugSkips`** - skip the test entirely (e.g. runtime differences like `error(nil)` behaving differently on native Lua vs WASM).
- **`tstlBugCodegenSkips`** - skip codegen comparison only. Used when tslua's output is correct but intentionally differs from TSTL (e.g. hex literals for bitmasks, better for-loop continue handling).
- **`batchDiagnosticOverrides`** - suppress diagnostic checks. The Go test harness compiles multiple test cases in a single `Program` for performance, which can cause `declare global` conflicts that don't exist in TSTL's per-test compilation.
- **`bakeLimitationOverrides`** - the expected value can't be computed by JS baking alone (e.g. Lua-specific error message formatting).

## Hand-written tests

`internal/luatest/` contains hand-written integration tests for cases where:

- The TSTL test uses an assertion pattern too complex to migrate automatically
- The Jest harness wiring is too fiddly for a specific scenario
- tslua has behavior that TSTL doesn't test (e.g. tslua-specific features like alternative class styles)

These tests follow the same pattern as migrated tests (transpile TypeScript, run Lua, check output) but are written directly in Go.

```bash
just test     # runs transpiler + lua + luatest
just testall  # runs all three packages including tstltest
```

## Lua runtimes

Both migrated and hand-written eval tests run the transpiled Lua against real Lua interpreters. `just lua-setup` builds all supported runtimes from source into `.lua-runtimes/bin/`:

| Binary   | Version | Source                                |
| -------- | ------- | ------------------------------------- |
| `lua5.0` | 5.0.3   | lua.org tarball                       |
| `lua5.1` | 5.1.5   | lua.org tarball                       |
| `lua5.2` | 5.2.4   | lua.org tarball                       |
| `lua5.3` | 5.3.6   | lua.org tarball                       |
| `lua5.4` | 5.4.7   | lua.org tarball                       |
| `lua5.5` | 5.5.0   | lua.org tarball                       |
| `luajit` | 2.1     | GitHub mirror, pinned commit          |
| `lune`   | 0.10.4  | Pre-built binary from GitHub releases |

Each `TestEval_*` case specifies a Lua target. The test harness selects the matching runtime and runs the transpiled output against it. Target-specific tests (e.g. bitwise operators on 5.3+, native continue on Luau) only run when the corresponding binary is available.

Lune provides Luau support. tslua emits Luau-specific constructs (e.g. native `continue`) when `luaTarget` is set to `Luau`, and these are verified against Lune.

```bash
just lua-setup   # build all runtimes (cached, ~30s first time)
```

## Test gate

`just testall` is the gate. All three test packages must pass:

```
$ just testall
  internal/lua
  internal/transpiler
  internal/tstltest
```
