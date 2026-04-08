---
title: Emit Mode
description: TSTL-compatible vs optimized Lua output.
---

tslua has two emit modes, controlled by the `emitMode` option in tsconfig.json or the `--emitMode` CLI flag.

```json
{
  "tstl": {
    "emitMode": "optimized"
  }
}
```

## `"tstl"` (default)

Matches TSTL's Lua output as closely as possible. Use this when you need byte-for-byte compatibility with TSTL, or when comparing output between the two transpilers.

## `"optimized"`

Emits cleaner Lua where tslua can prove the result is semantically equivalent. Every optimization preserves identical runtime behavior; the Lua evaluates to the same result as the default mode.

This mode is a work in progress. Current optimizations:

| Area | Default (`tstl`) | Optimized | Notes |
|------|-------------------|-----------|-------|
| `tostring()` in concat | Wraps all non-string operands including numeric vars | Skips wrapping for numeric types | Lua `..` handles numbers natively |
| C-style for loops | `while` loop with manual init/increment | `for i = start, limit` when pattern matches | Simpler, faster Lua |
| Map/Set for-of | Allocates intermediate tables | Zero-garbage stateless iteration via [custom lualib helpers](https://github.com/RealColdFry/tslua/blob/master/internal/lualib/patches.lua) | Less GC pressure |

More optimizations will be added over time. If you find a case where optimized mode changes runtime behavior, please [open an issue](https://github.com/RealColdFry/tslua/issues).
