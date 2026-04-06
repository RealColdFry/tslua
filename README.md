# tslua

Go reimplementation of [TypeScriptToLua](https://github.com/TypeScriptToLua/TypeScriptToLua) using [typescript-go](https://github.com/microsoft/typescript-go) internals.

Work in progress. Passes a large portion of TSTL's test suite but not yet a drop-in replacement.

Targets: LuaJIT, Lua 5.0–5.5, universal.

tslua ports from TSTL's architecture, transforms, and test suite.

## What's missing

tslua doesn't yet cover everything TSTL does. Notable gaps:

- **Plugins** - TSTL's `luaPlugins` hook system isn't ported. A Go binary can't load JS transformer plugins, so this needs a different approach. In practice, many TSTL plugins handle things like enum remapping, array proxy workarounds, and lualib patching. A native Go pipeline can address these differently since it transpiles lualib through the same path as user code. We're still figuring out what a plugin story looks like here.
- **Build modes** - `--buildMode library` not implemented.
- **Diagnostics** - not all TSTL diagnostics are implemented, and some differ due to using a different type checker.

## Background reading

- [Progress on TypeScript 7 (December 2025)](https://devblogs.microsoft.com/typescript/progress-on-typescript-7-december-2025/) — TS7 won't support the existing JS API; third-party tools (including TSTL plugins) face a migration
- [typescript-go: API design direction](https://github.com/microsoft/typescript-go/discussions/455) — IPC-based programmatic API over msgpack, no stable Go API planned
- [typescript-go: Public Go API for embedding?](https://github.com/microsoft/typescript-go/discussions/481) — whether typescript-go will expose stable Go packages for in-process use
- [typescript-go: Transformer plugin / compiler API](https://github.com/microsoft/typescript-go/issues/516) — ecosystem asking what happens to compiler plugins (TSTL, typia, Angular) under TS7
- [typescript-go: API patterns for editor extensions](https://github.com/microsoft/typescript-go/issues/2824) — concrete IPC API design replacing TS Server plugins (Vue case study)
