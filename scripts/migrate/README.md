# TSTL Test Migration

Extracts test cases from TSTL's Jest spec files and code-generates them into native Go tests under `internal/tstltest/`.

See the [testing docs](https://realcoldfry.github.io/tslua/testing/overview/#migrated-go-tests) for how this fits into tslua's overall test strategy.

## How it works

Each TSTL spec file is run in a sandboxed VM with mock `util.testExpression` / `util.testFunction` / `util.testModule` builders. Instead of executing the tests, these builders capture test structure (TypeScript source, options, expected values) and hand it off to the Go code generator.

## Pipeline

| File | Role |
|------|------|
| `cli.ts` | Entry point, spec discovery, cache pre-warming, `-c` check mode |
| `extract.ts` | Sandboxed spec execution, test case capture |
| `builder.ts` | Mock builder that records `.setOptions()`, `.ignoreDiagnostics()`, etc. |
| `evaluate.ts` | JS baking - transpiles TS to JS and runs it to get expected values |
| `tstl-ref.ts` | Runs code through TSTL to capture reference Lua for codegen comparison |
| `codegen.ts` | Emits Go test files with batch test cases |
| `constants.ts` | Overrides, skips, and target mappings |
| `types.ts` | `TestCase` type definition |
| `serialize.ts` | Go literal serialization |
| `migrate.ts` | Orchestrates extract + evaluate + codegen for a single spec |

## Usage

```bash
just migrate expressions    # migrate a specific TSTL spec
just migrate-all            # regenerate all migrated test files

# check migration coverage without generating files
node --require tsx/cjs scripts/migrate/cli.ts -c -a
```
