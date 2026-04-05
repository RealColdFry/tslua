---
title: tsconfig.json
description: Configuration reference for tslua's tsconfig.json options.
---

tslua reads standard `tsconfig.json` files. tslua-specific options go under the `"tstl"` key.

```json
{
  "compilerOptions": {
    "target": "ESNext",
    "lib": ["ESNext"],
    "strict": true
  },
  "tstl": {
    "luaTarget": "JIT"
  }
}
```

## tslua options

All options go under `"tstl"` in tsconfig.json.

| Option                      | Type   | Default     | Description                                                                                                     |
| --------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------------- |
| `luaTarget`                 | string | `"JIT"`     | Lua version: `JIT`, `5.0`-`5.5`, `universal`                                                                    |
| `emitMode`                  | string | `"tstl"`    | `"tstl"` (match TSTL output) or `"optimized"` (cleaner Lua)                                                     |
| `luaLibImport`              | string | `"require"` | How polyfills are included: `require`, `inline`, `none`                                                         |
| `noImplicitSelf`            | bool   | `false`     | Default all functions to no-self calling convention                                                             |
| `noImplicitGlobalVariables` | bool   | `false`     | Root-level declarations are `local` in non-module files                                                         |
| `exportAsGlobal`            | bool   | `false`     | Strip module wrapper, emit exports as globals                                                                   |
| `sourceMapTraceback`        | bool   | `false`     | Apply source maps to Lua stack traces                                                                           |
| `luaBundle`                 | string |             | Bundle all output into a single Lua file (requires `luaBundleEntry`)                                            |
| `luaBundleEntry`            | string |             | Entry point for bundle mode (requires `luaBundle`)                                                              |
| `classStyle`                | string |             | Class emit preset: `"tstl"`, `"luabind"`, `"middleclass"`, `"inline"`. See [Class Styles](/config/class-style/) |

## TypeScript compiler options

Standard `compilerOptions` work normally.

| Option                   | Recommendation             | Notes                                                                          |
| ------------------------ | -------------------------- | ------------------------------------------------------------------------------ |
| `target`                 | `"ESNext"`                 | Controls type checking, not Lua output. `ESNext` gives access to all features. |
| `lib`                    | `["ESNext"]`               | Required - provides types for `Promise`, `Map`, `Set`, etc.                    |
| `moduleResolution`       | `"bundler"`                |                                                                                |
| `strict`                 | `true`                     |                                                                                |
| `jsx`                    | `"react"` or `"react-jsx"` | For JSX support                                                                |
| `experimentalDecorators` | `true`                     | For legacy decorator syntax                                                    |

### Unsupported options

| Option                            | Notes                      |
| --------------------------------- | -------------------------- |
| `outFile`                         | Use `luaBundle` instead    |
| `importHelpers` / `noEmitHelpers` | Use `luaLibImport` instead |
| `composite` / `incremental`       | Not supported              |
| `emitDecoratorMetadata`           | Not supported              |
