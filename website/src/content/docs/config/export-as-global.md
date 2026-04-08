---
title: Export as Global
description: Strip module wrappers and emit exports as bare Lua globals.
---

By default, tslua wraps each module's exports in a `____exports` table and returns it, matching standard Lua module conventions. The `exportAsGlobal` option strips this wrapper and emits exported declarations as bare globals instead.

This is useful for embedded Lua environments where scripts run in a global scope rather than as `require()`-able modules.

## Usage

Set `exportAsGlobal` in tsconfig.json:

```json
{
  "tstl": {
    "exportAsGlobal": true
  }
}
```

Or use the CLI flag:

```bash
tslua -p tsconfig.json --exportAsGlobal
```

## Example

Given this TypeScript:

```typescript
export function greet(name: string) {
  return "Hello, " + name;
}
export const VERSION = "1.0";
```

**Default output** (module wrapper):

```lua
local ____exports = {}
function ____exports.greet(self, name)
    return "Hello, " .. name
end
____exports.VERSION = "1.0"
return ____exports
```

**With `exportAsGlobal: true`**:

```lua
local function greet(self, name)
    return "Hello, " .. name
end
VERSION = "1.0"
```

Exports become top-level declarations. Functions get `local` scope; constants are globals accessible to the host environment.

## Selective matching

Instead of a boolean, `exportAsGlobal` accepts a regex string to selectively apply to specific files:

```json
{
  "tstl": {
    "exportAsGlobal": "\\.script\\.ts$"
  }
}
```

This applies export-as-global only to files matching the pattern (e.g. `game.script.ts`), while other files (e.g. `util.ts`) keep their module wrappers. Useful when some files are entry-point scripts and others are shared modules.
