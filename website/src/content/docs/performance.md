---
title: Performance
description: Transpile speed and runtime benchmarks.
---

## Transpile speed

Measured on a game project (~180 source files, LuaJIT target) as of April 2026. Times vary by machine.

| Scenario                         | Time    |
| -------------------------------- | ------- |
| Initial build                    | ~550ms  |
| Incremental rebuild (watch mode) | ~6-18ms |

Initial build phase breakdown:

| Phase        | Time  |
| ------------ | ----- |
| Parse + bind | 113ms |
| Type check   | 203ms |
| Transform    | 216ms |
| Print        | 10ms  |
| Write        | 10ms  |

### Watch mode architecture

In watch mode (`tslua -p tsconfig.json --watch`), incremental rebuilds are fast because:

- **Incremental program update**: typescript-go's `incremental.NewProgram` diffs the changed file against its snapshot, avoiding a full reparse
- **Async diagnostics**: type-checking runs in a background goroutine after .lua files are written. Diagnostics arrive ~20-50ms later without blocking output
- **Scoped semantic check**: only the changed files are checked for import elision data, not the entire program
- **fsnotify**: file changes are detected via OS notifications, not polling

### Phase breakdown (incremental clean edit)

| Phase               | Time        |
| ------------------- | ----------- |
| Program update      | ~1-3ms      |
| Transform           | ~1-5ms      |
| Print               | ~0.1-0.5ms  |
| Write               | ~0.2-0.8ms  |
| **Build done**      | **~6-18ms** |
| Diagnostics (async) | +20-50ms    |

## Runtime benchmarks

These benchmarks compare `tstl` (default) vs `optimized` [emit mode](/tslua/config/emit-mode/) as of April 2026. Optimized emit mode is early and only covers a few patterns so far (iterator allocation, tostring elision). The numbers below reflect what's implemented today.

```bash
just bench              # run with LuaJIT (default)
just bench-lua          # show transpiled Lua output
```

### LuaJIT

| Benchmark     | Time (tstl) | Time (opt)     | Garbage (tstl) | Garbage (opt) |
| ------------- | ----------- | -------------- | -------------- | ------------- |
| array_entries | 0.238ms     | 0.028ms (8.5x) | 988 KB         | 128 KB (-87%) |
| map_iterate   | 0.080ms     | 0.016ms (5.0x) | 355 KB         | 96 KB (-73%)  |
| set_iterate   | 0.014ms     | 0.011ms (1.3x) | 65 KB          | 64 KB         |

### Lua 5.1

| Benchmark     | Time (tstl) | Time (opt)     | Garbage (tstl) | Garbage (opt) |
| ------------- | ----------- | -------------- | -------------- | ------------- |
| array_entries | 2.960ms     | 0.373ms (7.9x) | 2600 KB        | 256 KB (-90%) |
| map_iterate   | 0.974ms     | 0.463ms (2.1x) | 897 KB         | 193 KB (-78%) |
| set_iterate   | 0.807ms     | 0.389ms (2.1x) | 551 KB         | 129 KB (-77%) |

Other benchmarks (array_iterate, array_push, string_iterate, string_concat) show no significant difference between modes. Run the full suite with `just bench`.

The wins come from iterator optimizations in `optimized` emit mode. Map/Set for-of loops use custom stateless Lua iterators that walk the internal linked list directly, avoiding per-step closure and table allocations. See [Emit Mode](/tslua/config/emit-mode/) for details.
