---
title: CLI Reference
description: tslua command-line usage and flags.
---

## Project compilation

```bash
tslua -p tsconfig.json
tslua -p tsconfig.json --outdir dist
tslua -p tsconfig.json --watch
```

### Flags

| Flag                 | Description                                         | Default |
| -------------------- | --------------------------------------------------- | ------- |
| `-p, --project`      | Path to tsconfig.json                               |         |
| `--outdir`           | Output directory for Lua files                      | stdout  |
| `--luaTarget`        | Lua target version (JIT, 5.0-5.5, universal)        | JIT     |
| `--emitMode`         | `tstl` (match TSTL output) or `optimized`           | tstl    |
| `--luaLibImport`     | How lualib is included: `require`, `inline`, `none` | require |
| `--luaBundle`        | Bundle all modules into a single Lua file           |         |
| `--luaBundleEntry`   | Entry point for bundle mode                         |         |
| `--exportAsGlobal`   | Strip module wrapper, emit exports as globals       | false   |
| `--noImplicitSelf`   | Default functions to no-self unless annotated       | false   |
| `--sourceMap`        | Generate .lua.map source map files                  | false   |
| `-w, --watch`        | Watch for file changes and rebuild                  | false   |
| `--timing`           | Print phase timings to stderr                       | false   |
| `--verbose`          | Print each output file path                         | false   |
| `--diagnosticFormat` | Diagnostic format: `tstl` or `native`               | tstl    |
| `--cpuprofile`       | Write CPU profile to file                           |         |

## Commands

### `eval`

Transpile TypeScript to Lua. Accepts source via `-e` flag, positional argument, or stdin.

```bash
tslua eval -e 'const greet = (name: string) => `Hello, ${name}!`'
```

```lua
greet = function(____, name) return ("Hello, " .. name) .. "!" end
```

Loops and standard library calls are translated idiomatically:

```bash
echo 'for (const x of [1, 2, 3]) { console.log(x) }' | tslua eval
```

```lua
for ____, x in ipairs({1, 2, 3}) do
    print(x)
end
```

Classes produce full Lua object setup:

```bash
tslua eval -e 'class Dog { name: string; constructor(n: string) { this.name = n } bark() { return this.name + " barks" } }'
```

```lua
local ____lualib = require("lualib_bundle")
local __TS__Class = ____lualib.__TS__Class
Dog = __TS__Class()
Dog.name = "Dog"
function Dog.prototype.____constructor(self, n)
    self.name = n
end
function Dog.prototype.bark(self)
    return self.name .. " barks"
end
```

The `--trace` flag annotates each statement with the TS AST node that produced it:

```bash
tslua eval --trace -e 'const x = 1 + 2'
```

```lua
--[[trace: KindVariableStatement]]
x = 1 + 2
```

| Flag         | Description                                        |
| ------------ | -------------------------------------------------- |
| `-e, --expr` | TypeScript source to transpile                     |
| `--trace`    | Emit `--[[trace: ...]]` comments on each statement |

### `ast`

Print the TypeScript AST tree. Useful for debugging transforms.

```bash
tslua ast -e 'const x = [1, 2, 3]'
```

```
SourceFile
  VariableStatement
    VariableDeclarationList  const
      VariableDeclaration  name="x"
        Identifier  text="x"
        ArrayLiteralExpression
          NumericLiteral  text="1"
          NumericLiteral  text="2"
          NumericLiteral  text="3"
  EndOfFile
```

| Flag         | Description                |
| ------------ | -------------------------- |
| `-e, --expr` | TypeScript source to parse |

### `completion`

Generate shell autocompletion scripts.

```bash
# Bash
tslua completion bash > /etc/bash_completion.d/tslua

# Zsh
tslua completion zsh > "${fpath[1]}/_tslua"

# Fish
tslua completion fish > ~/.config/fish/completions/tslua.fish
```

### `server`

Run as a JSON-over-stdin/stdout server. Used by the TSTL test suite (`just tstl-test`) to avoid re-launching the binary per test case.

```bash
tslua server
tslua server --socket /tmp/tslua.sock
```

| Flag       | Description                              |
| ---------- | ---------------------------------------- |
| `--socket` | Unix socket path (default: stdin/stdout) |
