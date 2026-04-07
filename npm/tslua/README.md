# tslua

TypeScript-to-Lua transpiler built on [typescript-go](https://github.com/microsoft/typescript-go).

**Supported targets:** LuaJIT, Lua 5.0-5.5, and universal.

> [!WARNING]
> This is an early development build. Use at your own risk.

## Install

```bash
npm install @tslua/cli
```

## Usage

```bash
# Transpile a project
npx tslua -p tsconfig.json

# Transpile inline code
npx tslua eval -e 'const x: number = 1; print(x)'
```

## Binary downloads

Pre-built binaries are also available on [GitHub Releases](https://github.com/RealColdFry/tslua/releases).
