---
title: Installation
description: How to install and use tslua.
---

## npm

```bash
npm i @tslua/cli
npx tslua eval -e 'print("hello")'
```

## From source

Requires Go 1.24+, Node 20+, and [just](https://github.com/casey/just).

```bash
git clone https://github.com/RealColdFry/tslua
cd tslua
just setup
just build
./tslua eval -e 'print("hello")'
```

## Usage

```bash
# Transpile a project
tslua -p tsconfig.json

# Transpile inline code
tslua eval -e 'const x: number = 1 + 2'

# Print the TypeScript AST
tslua ast -e 'const x = [1, 2, 3]'
```

See the [CLI reference](/tslua/cli/overview/) for all commands and flags.

## tsconfig.json

tslua reads standard `tsconfig.json` files. tslua-specific options go under the `"tstl"` key:

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

See [tsconfig.json reference](/tslua/config/tsconfig/) for all options.
