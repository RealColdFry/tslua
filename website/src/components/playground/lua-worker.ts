// Web Worker that executes Lua code off the main thread.
// Receives { code, target } messages, returns { raw, pretty } results.
// All WASM loading and execution happens here.

// lua-wasm-bindings has no declarations for the binding-factory entry but
// does export Emscripten module types we can use for the glue factories.

// @ts-expect-error — binding-factory has no declarations upstream
import { createLua, createLauxLib, createLuaLib } from "lua-wasm-bindings/dist/binding-factory";
import type { LuaEmscriptenModule } from "lua-wasm-bindings/dist/glue/glue";

import glueFactory50 from "lua-wasm-bindings/dist/glue/glue-lua-5.0.3.js";
import glueFactory51 from "lua-wasm-bindings/dist/glue/glue-lua-5.1.5.js";
import glueFactory52 from "lua-wasm-bindings/dist/glue/glue-lua-5.2.4.js";
import glueFactory53 from "lua-wasm-bindings/dist/glue/glue-lua-5.3.6.js";
import glueFactory54 from "lua-wasm-bindings/dist/glue/glue-lua-5.4.7.js";
import glueFactory55 from "lua-wasm-bindings/dist/glue/glue-lua-5.5.0.js";

import wasm50 from "lua-wasm-bindings/dist/glue/glue-lua-5.0.3.wasm?url";
import wasm51 from "lua-wasm-bindings/dist/glue/glue-lua-5.1.5.wasm?url";
import wasm52 from "lua-wasm-bindings/dist/glue/glue-lua-5.2.4.wasm?url";
import wasm53 from "lua-wasm-bindings/dist/glue/glue-lua-5.3.6.wasm?url";
import wasm54 from "lua-wasm-bindings/dist/glue/glue-lua-5.4.7.wasm?url";
import wasm55 from "lua-wasm-bindings/dist/glue/glue-lua-5.5.0.wasm?url";

// --- Narrow types for the bindings surface we use ---

type LuaState = number;

interface LuaLib {
  lua_tostring(L: LuaState, idx: number): string;
  lua_close(L: LuaState): void;
  lua_pcall(L: LuaState, nargs: number, nresults: number, msgh: number): number;
  lua_getglobal(L: LuaState, name: string): number;
  lua_getfield(L: LuaState, index: number, k: string): number;
  lua_remove(L: LuaState, index: number): void;
  lua_gettop(L: LuaState): number;
  lua_pop(L: LuaState, n: number): void;
}

interface LauxLib {
  luaL_newstate(): LuaState;
  luaL_dostring(L: LuaState, code: string): number;
  luaL_loadstring(L: LuaState, code: string): number;
}

interface LuaStdLib {
  luaL_openlibs(L: LuaState): void;
}

type GlueFactory = (opts: {
  wasmBinary: Uint8Array;
  print(text: string): void;
  printErr(text: string): void;
}) => LuaEmscriptenModule;

const createLuaTyped = createLua as (glue: LuaEmscriptenModule, semver: string) => LuaLib;
const createLauxLibTyped = createLauxLib as (
  glue: LuaEmscriptenModule,
  lua: LuaLib,
  semver: string,
) => LauxLib;
const createLuaLibTyped = createLuaLib as (glue: LuaEmscriptenModule, semver: string) => LuaStdLib;

interface ExecResult {
  output: string[];
  error: string | null;
}

// --- Pretty print preambles ---

const PRETTY_PRINT_PREAMBLE_50 = `
local function jsString(v)
  if type(v) == "table" then
    local n = table.getn(v)
    local isArray = true
    if n == 0 then
      isArray = false
    else
      for i = 1, n do
        if v[i] == nil then isArray = false; break end
      end
    end
    if isArray then
      local parts = {}
      for i = 1, n do parts[i] = jsString(v[i]) end
      return table.concat(parts, ",")
    else
      local parts = {}
      for k, val in pairs(v) do
        table.insert(parts, tostring(k) .. ": " .. jsString(val))
      end
      table.sort(parts)
      return "{ " .. table.concat(parts, ", ") .. " }"
    end
  end
  return tostring(v)
end
local _origPrint = print
print = function(...)
  local args = arg
  local parts = {}
  for i = 1, table.getn(args) do
    parts[i] = jsString(args[i])
  end
  _origPrint(table.concat(parts, " "))
end
console = {
  log = function(_, ...) print(...) end,
  warn = function(_, ...) print(...) end,
  error = function(_, ...) print(...) end,
  info = function(_, ...) print(...) end,
  debug = function(_, ...) print(...) end,
  trace = function(_, ...) print(...) end,
  assert = function(_, ...) print(...) end,
}
`;

const PRETTY_PRINT_PREAMBLE = `
local function jsString(v)
  if type(v) == "table" then
    local n = #v
    local isArray = true
    if n == 0 then
      isArray = false
    else
      for i = 1, n do
        if v[i] == nil then isArray = false; break end
      end
    end
    if isArray then
      local parts = {}
      for i = 1, n do parts[i] = jsString(v[i]) end
      return table.concat(parts, ",")
    else
      local parts = {}
      for k, val in pairs(v) do
        parts[#parts+1] = tostring(k) .. ": " .. jsString(val)
      end
      table.sort(parts)
      return "{ " .. table.concat(parts, ", ") .. " }"
    end
  end
  return tostring(v)
end
local _origPrint = print
print = function(...)
  local args = {}
  for i = 1, select("#", ...) do
    args[i] = jsString(select(i, ...))
  end
  _origPrint(table.concat(args, " "))
end
console = {
  log = function(_, ...) print(...) end,
  warn = function(_, ...) print(...) end,
  error = function(_, ...) print(...) end,
  info = function(_, ...) print(...) end,
  debug = function(_, ...) print(...) end,
  trace = function(_, ...) print(...) end,
  assert = function(_, ...) print(...) end,
}
`;

function getPreamble(target: string): string {
  return target === "5.0" ? PRETTY_PRINT_PREAMBLE_50 : PRETTY_PRINT_PREAMBLE;
}

// --- WASM module management ---

const TARGET_TO_VERSION: Record<string, string> = {
  JIT: "5.1",
  "5.0": "5.0",
  "5.1": "5.1",
  "5.2": "5.2",
  "5.3": "5.3",
  "5.4": "5.4",
  "5.5": "5.5",
  universal: "5.4",
};

interface VersionInfo {
  factory: GlueFactory | { default: GlueFactory };
  wasmUrl: string;
  semver: string;
}

const VERSIONS: Record<string, VersionInfo> = {
  "5.0": { factory: glueFactory50 as GlueFactory, wasmUrl: wasm50 as string, semver: "5.0.3" },
  "5.1": { factory: glueFactory51 as GlueFactory, wasmUrl: wasm51 as string, semver: "5.1.5" },
  "5.2": { factory: glueFactory52 as GlueFactory, wasmUrl: wasm52 as string, semver: "5.2.4" },
  "5.3": { factory: glueFactory53 as GlueFactory, wasmUrl: wasm53 as string, semver: "5.3.6" },
  "5.4": { factory: glueFactory54 as GlueFactory, wasmUrl: wasm54 as string, semver: "5.4.7" },
  "5.5": { factory: glueFactory55 as GlueFactory, wasmUrl: wasm55 as string, semver: "5.5.0" },
};

let currentOutput: string[] = [];

interface LuaModule {
  lua: LuaLib;
  lauxlib: LauxLib;
  lualib: LuaStdLib;
}

const moduleCache = new Map<string, LuaModule>();

async function getLuaModule(version: string): Promise<LuaModule> {
  const cached = moduleCache.get(version);
  if (cached) return cached;

  const ver = VERSIONS[version];
  if (!ver) throw new Error(`No Lua ${version} WASM available`);

  const wasmResponse = await fetch(ver.wasmUrl);
  const wasmBinary = new Uint8Array(await wasmResponse.arrayBuffer());

  const factory: GlueFactory =
    "default" in ver.factory && typeof ver.factory.default === "function"
      ? ver.factory.default
      : (ver.factory as GlueFactory);
  const luaGlue = factory({
    wasmBinary,
    print: (text: string) => {
      currentOutput.push(text);
    },
    printErr: (text: string) => {
      currentOutput.push("[stderr] " + text);
    },
  });

  const lua = createLuaTyped(luaGlue, ver.semver);
  const lauxlib = createLauxLibTyped(luaGlue, lua, ver.semver);
  const lualib = createLuaLibTyped(luaGlue, ver.semver);

  const mod: LuaModule = { lua, lauxlib, lualib };
  moduleCache.set(version, mod);
  return mod;
}

// Lua's default chunkname for `luaL_loadstring` is the source string itself,
// which surfaces as `[string "...truncated source..."]:N:` in error messages.
// Rewrite it to a filename-like prefix so errors read like a local
// `lua file.lua` run. Applies to the traceback's stack frames too.
// `debug.traceback` indents stack frames with a literal tab; rewrite to
// a fixed number of spaces so the playground renders consistently.
const TRACEBACK_INDENT = "  ";
function formatError(raw: string | null): string {
  if (!raw) return "Lua execution error";
  return raw.replace(/\[string "[^"]*"\]/g, "main").replace(/\t/g, TRACEBACK_INDENT);
}

function runOnce(
  lua: LuaLib,
  lauxlib: LauxLib,
  lualib: LuaStdLib,
  setupChunks: string[],
  code: string,
): ExecResult {
  currentOutput = [];
  try {
    const L = lauxlib.luaL_newstate();
    lualib.luaL_openlibs(L);

    // Run each setup chunk in its own `luaL_dostring` call so user-code line
    // numbers aren't shifted by prepended preludes.
    for (const chunk of setupChunks) {
      const setupErr = lauxlib.luaL_dostring(L, chunk);
      if (setupErr !== 0) {
        const errMsg = lua.lua_tostring(L, -1);
        lua.lua_close(L);
        return { output: [...currentOutput], error: formatError(errMsg) };
      }
    }

    // Push `debug.traceback` as the message handler so errors come back with
    // a stack trace, matching what a local `lua file.lua` run prints.
    lua.lua_getglobal(L, "debug");
    lua.lua_getfield(L, -1, "traceback");
    lua.lua_remove(L, -2);
    const msghIdx = lua.lua_gettop(L);

    const loadErr = lauxlib.luaL_loadstring(L, code);
    if (loadErr !== 0) {
      const errMsg = lua.lua_tostring(L, -1);
      lua.lua_close(L);
      return { output: [...currentOutput], error: formatError(errMsg) };
    }

    const callErr = lua.lua_pcall(L, 0, 0, msghIdx);
    if (callErr !== 0) {
      const errMsg = lua.lua_tostring(L, -1);
      lua.lua_close(L);
      return { output: [...currentOutput], error: formatError(errMsg) };
    }

    lua.lua_close(L);
    return { output: [...currentOutput], error: null };
  } catch (e) {
    return { output: [...currentOutput], error: String(e) };
  }
}

// --- Message handler ---

export interface LuaWorkerRequest {
  id: number;
  code: string;
  target: string;
  lualib?: string;
}

export interface LuaWorkerResponse {
  id: number;
  raw: ExecResult;
  pretty: ExecResult;
}

// Wraps `bundleCode` so that `require("lualib_bundle")` returns the table the
// bundle's `return { ... }` produces. Uses a long bracket with enough equals
// signs to avoid collision with anything the bundle itself could contain.
function buildLualibPrelude(bundleCode: string): string {
  let eqs = "=====";
  while (bundleCode.includes("]" + eqs + "]")) eqs += "=";
  const open = "[" + eqs + "[";
  const close = "]" + eqs + "]";
  return `do
  local ____lualib_chunk = assert(loadstring or load)(${open}
${bundleCode}
${close}, "lualib_bundle")
  local ____lualib_table = ____lualib_chunk()
  local ____orig_require = require
  require = function(name)
    if name == "lualib_bundle" then return ____lualib_table end
    return ____orig_require(name)
  end
end
`;
}

self.addEventListener("message", async (e: MessageEvent<LuaWorkerRequest>) => {
  const { id, code, target, lualib: lualibBundle } = e.data;
  const version = TARGET_TO_VERSION[target] ?? "5.4";

  try {
    const { lua, lauxlib, lualib } = await getLuaModule(version);
    const setup: string[] = [];
    if (lualibBundle) setup.push(buildLualibPrelude(lualibBundle));
    const raw = runOnce(lua, lauxlib, lualib, setup, code);
    const pretty = runOnce(lua, lauxlib, lualib, [...setup, getPreamble(target)], code);
    self.postMessage({ id, raw, pretty } satisfies LuaWorkerResponse);
  } catch (err) {
    const errResult: ExecResult = { output: [], error: String(err) };
    self.postMessage({ id, raw: errResult, pretty: errResult } satisfies LuaWorkerResponse);
  }
});
