// Setup-chunk builders and pretty-print preambles for the playground's Lua
// worker. Kept in a separate module (no DOM, no WASM, no `?raw` imports) so
// it can be unit-tested directly against `lua-wasm-bindings`.

export const TARGET_TO_VERSION: Record<string, string> = {
  JIT: "5.1",
  "5.0": "5.0",
  "5.1": "5.1",
  "5.2": "5.2",
  "5.3": "5.3",
  "5.4": "5.4",
  "5.5": "5.5",
  universal: "5.4",
};

// Encode arbitrary source as a Lua source literal. For 5.1+ we use a leveled
// long bracket (cheap, preserves the source verbatim). Lua 5.0 has no leveled
// long brackets and embedded source can contain `]]`, so we fall back to an
// escaped double-quoted string built byte-by-byte from the UTF-8 encoding.
export function luaSourceLiteral(source: string, target: string): string {
  if (target !== "5.0") {
    let eqs = "=====";
    while (source.includes("]" + eqs + "]")) eqs += "=";
    return "[" + eqs + "[\n" + source + "\n]" + eqs + "]";
  }
  const bytes = new TextEncoder().encode(source);
  let out = '"';
  for (const b of bytes) {
    if (b === 0x5c) out += "\\\\";
    else if (b === 0x22) out += '\\"';
    else if (b === 0x0a) out += "\\n";
    else if (b === 0x0d) out += "\\r";
    else if (b === 0x09) out += "\\t";
    else if (b >= 0x20 && b < 0x7f) out += String.fromCharCode(b);
    else out += "\\" + b.toString().padStart(3, "0");
  }
  return out + '"';
}

// Wraps `bundleCode` so that `require("lualib_bundle")` returns the table the
// bundle's `return { ... }` produces.
export function buildLualibPrelude(bundleCode: string, target: string): string {
  return `do
  local ____lualib_chunk = assert(loadstring or load)(${luaSourceLiteral(bundleCode, target)}, "lualib_bundle")
  local ____lualib_table = ____lualib_chunk()
  local ____orig_require = require
  require = function(name)
    if name == "lualib_bundle" then return ____lualib_table end
    return ____orig_require(name)
  end
end
`;
}

// Registers the kikito/middleclass module under `package.loaded["middleclass"]`
// so the transpiler-injected `local class = require("middleclass")` header
// resolves when middleclass class-style output runs in the playground.
// Not used on Lua 5.0 (middleclass uses 5.1+ `...` varargs); the worker
// skips this prelude there.
export function buildMiddleclassPrelude(moduleCode: string, target: string): string {
  return `do
  local ____mc_chunk = assert(loadstring or load)(${luaSourceLiteral(moduleCode, target)}, "middleclass")
  package.loaded["middleclass"] = ____mc_chunk()
end
`;
}

// Pretty-print preamble installed before the user's Lua code runs. Wraps
// `print` so tables come out as JS-style "{ k: v, ... }" / "[a,b,c]" rather
// than `table: 0x...`, and registers a `console` shim.
//
// Lua 5.0 needs a separate body: `select(...)`, `#table`, and referencing
// `...` in expressions are all 5.1+ features. On 5.0 we use the implicit
// `arg` table and `table.getn`, and we forward varargs via `unpack(arg)`.
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
  log = function(_, ...) print(unpack(arg)) end,
  warn = function(_, ...) print(unpack(arg)) end,
  error = function(_, ...) print(unpack(arg)) end,
  info = function(_, ...) print(unpack(arg)) end,
  debug = function(_, ...) print(unpack(arg)) end,
  trace = function(_, ...) print(unpack(arg)) end,
  assert = function(_, ...) print(unpack(arg)) end,
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

export function getPrettyPrintPreamble(target: string): string {
  return target === "5.0" ? PRETTY_PRINT_PREAMBLE_50 : PRETTY_PRINT_PREAMBLE;
}
