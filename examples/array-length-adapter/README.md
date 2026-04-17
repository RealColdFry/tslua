# array-length-adapter

Inspection example for the runtime-adapter kernel. A user-declared
`@luaArrayRuntime` primitive replaces `arr.length` emit with `Len(arr)` in
both user code and the lualib bundle.

Motivating scenario: an embedded Lua host whose arrays are proxy objects with
host-tracked length, where `#` returns raw table length and misses the proxy's
tracked length. The user supplies a `Len(arr)` free function; tslua routes
every `arr.length` emit through it.

## Layout

```
array-length-adapter/
  tsconfig.json
  src/main.ts          # user code (unchanged from typical TS)
  vendor/
    runtime.d.ts       # @luaArrayRuntime declaration + declare function Len
    runtime.lua        # stand-in Lua impl of Len (rawlen passthrough)
  out/                 # transpiled output
```

## Regenerate

From the repo root:

```
go build -o tslua ./cmd/tslua
./tslua -p examples/array-length-adapter/tsconfig.json
```

Produces `out/main.lua` (user code) and `out/lualib_bundle.lua` (lualib rebuilt
from TypeScript source with the adapter applied).

## What to look for

**User code**: every `arr.length` in `src/main.ts` emits `Len(arr)` instead of
`#items`.

**Lualib bundle**: internal reads of `arr.length` inside `__TS__ArrayPush`,
`__TS__ArrayFilter`, etc. also emit `Len(arr)`:

```lua
local function __TS__ArrayPush(self, ...)
    local items = {...}
    local len = Len(self)
    for i = 1, Len(items) do
        len = len + 1
        self[len] = items[i]
    end
    return len
end
```

Non-adapter builds use the embedded pre-built bundle (zero cost). When an
adapter is active, the bundle is rebuilt from `extern/tstl/src/lualib/` on
every compile (~80 ms).

## Known gap: inline push fast-paths

The kernel wires `arr.length` reads but not the inline `arr[#arr + 1] = val`
idiom used by tslua's `push` single-arg fast path
(`internal/transpiler/builtins.go`) and a few TSTL lualib functions
(`Promise`, `ObjectGroupBy`, etc.). For a proxy-array host, these sites read
raw table length and write to the wrong slot. Grep the output for `#result`,
`#results`, `#pending`, `#rejections`, `#____` to see them. Follow-up: route
the push fast-path through the adapter, or add a dedicated push primitive.

## Running the output

The emitted `main.lua` requires `Len` to be in scope:

```
cd examples/array-length-adapter/out
lua -e 'dofile("../vendor/runtime.lua"); dofile("main.lua")'
```

## Declaration form

```typescript
// vendor/runtime.d.ts
declare function Len(arr: readonly unknown[]): number;

/** @luaArrayRuntime */
declare const HostArrayRuntime: {
    length: typeof Len;
};
```

tslua scans for the `@luaArrayRuntime` JSDoc; the type literal lists the
primitives provided; `typeof Len` names the Lua identifier to emit. The
signature is validated at compile time (one array-typed param, number return);
a mismatch produces `RuntimeAdapterInvalidSignature` and leaves the default
emit in place.
