// Web Worker that executes Lua code off the main thread.
// Receives { code, target } messages, returns { raw, pretty } results.
// All WASM loading and execution happens here.

// @ts-ignore — CJS module
import { createLua, createLauxLib, createLuaLib } from "lua-wasm-bindings/dist/binding-factory";

// @ts-ignore
import glueFactory50 from "lua-wasm-bindings/dist/glue/glue-lua-5.0.3.js";
// @ts-ignore
import glueFactory51 from "lua-wasm-bindings/dist/glue/glue-lua-5.1.5.js";
// @ts-ignore
import glueFactory52 from "lua-wasm-bindings/dist/glue/glue-lua-5.2.4.js";
// @ts-ignore
import glueFactory53 from "lua-wasm-bindings/dist/glue/glue-lua-5.3.6.js";
// @ts-ignore
import glueFactory54 from "lua-wasm-bindings/dist/glue/glue-lua-5.4.7.js";
// @ts-ignore
import glueFactory55 from "lua-wasm-bindings/dist/glue/glue-lua-5.5.0.js";

// @ts-ignore
import wasm50 from "lua-wasm-bindings/dist/glue/glue-lua-5.0.3.wasm?url";
// @ts-ignore
import wasm51 from "lua-wasm-bindings/dist/glue/glue-lua-5.1.5.wasm?url";
// @ts-ignore
import wasm52 from "lua-wasm-bindings/dist/glue/glue-lua-5.2.4.wasm?url";
// @ts-ignore
import wasm53 from "lua-wasm-bindings/dist/glue/glue-lua-5.3.6.wasm?url";
// @ts-ignore
import wasm54 from "lua-wasm-bindings/dist/glue/glue-lua-5.4.7.wasm?url";
// @ts-ignore
import wasm55 from "lua-wasm-bindings/dist/glue/glue-lua-5.5.0.wasm?url";

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
  factory: any;
  wasmUrl: string;
  semver: string;
}

const VERSIONS: Record<string, VersionInfo> = {
  "5.0": { factory: glueFactory50, wasmUrl: wasm50, semver: "5.0.3" },
  "5.1": { factory: glueFactory51, wasmUrl: wasm51, semver: "5.1.5" },
  "5.2": { factory: glueFactory52, wasmUrl: wasm52, semver: "5.2.4" },
  "5.3": { factory: glueFactory53, wasmUrl: wasm53, semver: "5.3.6" },
  "5.4": { factory: glueFactory54, wasmUrl: wasm54, semver: "5.4.7" },
  "5.5": { factory: glueFactory55, wasmUrl: wasm55, semver: "5.5.0" },
};

let currentOutput: string[] = [];

interface LuaModule {
  lua: any;
  lauxlib: any;
  lualib: any;
}

const moduleCache = new Map<string, LuaModule>();

async function getLuaModule(version: string): Promise<LuaModule> {
  if (moduleCache.has(version)) return moduleCache.get(version)!;

  const ver = VERSIONS[version];
  if (!ver) throw new Error(`No Lua ${version} WASM available`);

  const wasmResponse = await fetch(ver.wasmUrl);
  const wasmBinary = new Uint8Array(await wasmResponse.arrayBuffer());

  const factory = ver.factory.default ?? ver.factory;
  const luaGlue = factory({
    wasmBinary,
    print: (text: string) => {
      currentOutput.push(text);
    },
    printErr: (text: string) => {
      currentOutput.push("[stderr] " + text);
    },
  });

  const lua = createLua(luaGlue, ver.semver);
  const lauxlib = createLauxLib(luaGlue, lua, ver.semver);
  const lualib = createLuaLib(luaGlue, ver.semver);

  const mod = { lua, lauxlib, lualib };
  moduleCache.set(version, mod);
  return mod;
}

function runOnce(lua: any, lauxlib: any, lualib: any, code: string): ExecResult {
  currentOutput = [];
  try {
    const L = lauxlib.luaL_newstate();
    lualib.luaL_openlibs(L);

    const err = lauxlib.luaL_dostring(L, code);
    if (err !== 0) {
      const errMsg = lua.lua_tostring(L, -1);
      lua.lua_close(L);
      return { output: [...currentOutput], error: errMsg || "Lua execution error" };
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
}

export interface LuaWorkerResponse {
  id: number;
  raw: ExecResult;
  pretty: ExecResult;
}

self.addEventListener("message", async (e: MessageEvent<LuaWorkerRequest>) => {
  const { id, code, target } = e.data;
  const version = TARGET_TO_VERSION[target] ?? "5.4";

  try {
    const { lua, lauxlib, lualib } = await getLuaModule(version);
    const raw = runOnce(lua, lauxlib, lualib, code);
    const pretty = runOnce(lua, lauxlib, lualib, getPreamble(target) + code);
    self.postMessage({ id, raw, pretty } satisfies LuaWorkerResponse);
  } catch (err) {
    const errResult: ExecResult = { output: [], error: String(err) };
    self.postMessage({ id, raw: errResult, pretty: errResult } satisfies LuaWorkerResponse);
  }
});
