---
title: Background
description: Context on TypeScript 7, typescript-go, and the future of TypeScript-to-Lua tooling.
---

tslua exists because TypeScript is being rewritten in Go. This page collects the key discussions and decisions shaping that transition, particularly for tools that depend on TypeScript's compiler API.

## Approaches to TS7 integration

Tools that depend on TypeScript's compiler API have a few options for the TS7 transition:

**IPC API consumer.** Wait for typescript-go's planned msgpack-based IPC API, then call it from any language (JS, Go, Rust, etc.) as a subprocess. This is the officially supported path. The API surface is still being designed ([discussion #455](https://github.com/microsoft/typescript-go/discussions/455)), and it's unclear how much of the type checker and AST will be exposed; editor tooling is the priority, not compiler plugins. A transpiler like TSTL needs deep access to types, symbols, and the full AST, which may or may not be available over IPC.

**Fork typescript-go.** Fork the Go codebase and add transpilation directly. Full control, but a large maintenance surface to keep in sync with upstream.

**Direct linking via shims.** Link against typescript-go's internals without forking, using `go:linkname` to access unexported APIs. This is what tslua does.

### How tslua uses typescript-go

tslua takes the third approach. A [`gen_shims`](https://github.com/RealColdFry/tslua/tree/master/tools/gen_shims) tool (originally from [tsgolint](https://github.com/oxc-project/tsgolint), now maintained in-tree) generates `go:linkname` shims that expose typescript-go's type checker, AST, and program APIs as importable Go packages. tslua gets full access to the same data structures that typescript-go uses internally, at the cost of tracking upstream changes as typescript-go evolves. There is no IPC overhead or subprocess; tslua is a single binary with the type checker compiled in.

## Reading list

- [Progress on TypeScript 7 (December 2025)](https://devblogs.microsoft.com/typescript/progress-on-typescript-7-december-2025/). TS7 won't support the existing JS API; third-party tools (including TSTL plugins) face a migration.
- [typescript-go: API design direction](https://github.com/microsoft/typescript-go/discussions/455). IPC-based programmatic API over msgpack, no stable Go API planned.
- [typescript-go: Public Go API for embedding?](https://github.com/microsoft/typescript-go/discussions/481). Whether typescript-go will expose stable Go packages for in-process use.
- [typescript-go: Transformer plugin / compiler API](https://github.com/microsoft/typescript-go/issues/516). Ecosystem asking what happens to compiler plugins (TSTL, typia, Angular) under TS7.
- [typescript-go: API patterns for editor extensions](https://github.com/microsoft/typescript-go/issues/2824). Concrete IPC API design replacing TS Server plugins (Vue case study).
