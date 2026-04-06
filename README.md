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
- **Source map traceback** - not implemented.
