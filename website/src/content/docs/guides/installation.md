---
title: Installation
description: How to install and use tslua.
---

## From source

```bash
git clone https://github.com/RealColdFry/tslua
cd tslua
just build
```

## Usage

```bash
# Transpile a project
./tslua -p tsconfig.json

# Transpile inline code
./tslua eval -e 'const x: number = 1 + 2'

# Print the TypeScript AST
./tslua ast -e 'const x = [1, 2, 3]'
```

## tsconfig.json

tslua reads standard `tsconfig.json` files. TSTL-specific options go under the `"tstl"` key:

```json
{
  "compilerOptions": {
    "target": "ES2017",
    "lib": ["ESNext"],
    "strict": true
  },
  "tstl": {
    "luaTarget": "JIT"
  }
}
```
