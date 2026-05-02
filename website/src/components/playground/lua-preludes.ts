// Setup-chunk builders for the playground's Lua worker. Kept in a separate
// module (no DOM, no WASM, no `?raw` imports) so it can be exercised from a
// plain Node script that pipes the output through the real `lua5.X` binaries.

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
