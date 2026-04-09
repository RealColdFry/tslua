// Bundle lua-types and language-extensions .d.ts files for the playground.
// Resolves /// <reference path="..."> directives into a single flat file per target.
//
// Usage: node scripts/bundle-types.js
//
// Reads from website/node_modules/, writes to website/src/assets/types/

const fs = require("fs");
const path = require("path");

const WEBSITE = path.resolve(__dirname, "../website");
const LUA_TYPES = path.join(WEBSITE, "node_modules/lua-types");
const LANG_EXT = path.join(
  WEBSITE,
  "node_modules/@typescript-to-lua/language-extensions/index.d.ts",
);
const OUT_DIR = path.join(WEBSITE, "src/assets/types");

const TARGETS = ["jit", "5.0", "5.1", "5.2", "5.3", "5.4", "5.5"];

function resolveRefs(file, seen) {
  const abs = path.resolve(file);
  if (seen.has(abs)) return "";
  seen.add(abs);
  const content = fs.readFileSync(file, "utf8");
  let out = "";
  for (const line of content.split("\n")) {
    const refPath = line.match(/\/\/\/\s*<reference\s+path="([^"]+)"/);
    if (refPath) {
      out += resolveRefs(path.resolve(path.dirname(file), refPath[1]), seen);
      continue;
    }
    // Skip /// <reference types="..."> (e.g. language-extensions)
    if (/\/\/\/\s*<reference\s+types="/.test(line)) continue;
    out += line + "\n";
  }
  return out;
}

fs.mkdirSync(OUT_DIR, { recursive: true });

// Bundle language-extensions (target-independent)
const langExt = fs.readFileSync(LANG_EXT, "utf8");
fs.writeFileSync(path.join(OUT_DIR, "language-extensions.d.ts"), langExt);
console.log(`language-extensions: ${langExt.length} bytes`);

// Bundle lua-types per target
for (const target of TARGETS) {
  const entry = path.join(LUA_TYPES, target === "jit" ? "jit.d.ts" : `${target}.d.ts`);
  const bundle = resolveRefs(entry, new Set());
  const outFile = `lua-types-${target}.d.ts`;
  fs.writeFileSync(path.join(OUT_DIR, outFile), bundle);
  console.log(`${outFile}: ${bundle.length} bytes`);
}
