# tslua

TypeScript-to-Lua transpiler built on [typescript-go](https://github.com/microsoft/typescript-go). Single binary, no Node runtime required.

Built on the architecture and test suite of [TypeScriptToLua](https://github.com/TypeScriptToLua/TypeScriptToLua). For general TypeScript-to-Lua usage and caveats, see the [TSTL docs](https://typescripttolua.github.io/). Targets LuaJIT and Lua 5.0-5.5.

[Docs](https://realcoldfry.github.io/tslua/) · [Playground](https://realcoldfry.github.io/tslua/playground/) · [CLI reference](https://realcoldfry.github.io/tslua/cli/overview/)

## Install

```bash
npm i @tslua/cli
```

## Try it

```ts
// tslua eval -e
const items = [10, 20, 30];
for (const x of items) { print(x * 2) }

// output:
// items = {10, 20, 30}
// for ____, x in ipairs(items) do
//     print(x * 2)
// end
```

## Why tslua

- **Native Go binary.** Uses typescript-go's type checker and AST directly via `go:linkname` shims, no IPC or JS runtime in the loop.
- **TSTL-compatible.** Ports TSTL's transforms and lualib faithfully. Reads the same `tsconfig.json` options and produces compatible output.
- **TS 7 ready.** Built on the compiler that TypeScript is migrating to.
- **Fast.** ~6-18ms incremental rebuilds in watch mode. [Benchmarks](https://realcoldfry.github.io/tslua/performance/)
- **Alternative class styles.** `tstl` (default, TSTL-compatible), `inline`, `luabind`, `middleclass`.

## Compatibility

Two verification approaches, both running TSTL's own tests:

- **[Jest harness](https://realcoldfry.github.io/tslua/testing/overview/#jest-harness).** TSTL's Jest suite runs unmodified, but with tslua's transpiler swapped in via a Unix socket server. **6071 / 6179 tests pass (98.3%).**
- **[Migrated Go tests](https://realcoldfry.github.io/tslua/testing/overview/#migrated-go-tests).** A migration system extracts TSTL's Jest specs and code-generates them into native Go tests. **5656 / 5903 cases migrated (95.8%)** across 70 of 71 spec files, with **100% behavioral pass rate** on migrated cases. The 247 unmigrated cases use TSTL assertion methods (`getMainLuaCodeChunk`, `getLuaExecutionResult`, etc.) not yet supported by the migration script.

## What's not done yet

- **Plugins.** TSTL's `luaPlugins` hook system. A Go binary can't load JS transformer plugins; the plugin story needs a different shape.
- **Build modes.** `--buildMode library` not implemented.
- **Diagnostics.** Not all TSTL diagnostics are ported, and some differ due to typescript-go's type checker.

## Building from source

Requires Go 1.24+, Node 20+, and [just](https://github.com/casey/just).

```bash
git clone https://github.com/RealColdFry/tslua
cd tslua
just setup
just build
./tslua eval -e 'print("hello")'
```

