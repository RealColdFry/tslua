// Verifies the playground worker's setup-chunk builders produce code that
// every supported Lua runtime can syntactically parse. Catches Lua-version
// syntax mismatches (e.g. leveled long brackets `[=[...]=]` that exist on
// 5.1+ but not on 5.0) before they reach the playground.
//
// Uses `lua -e 'assert((loadstring or load)(...))'` rather than running the
// chunk: the CLI `lua5.0` binary doesn't initialize the same stdlib surface
// as the WASM build (no `package` global out of the box), so a runtime check
// would produce false positives. Parse-only is the right scope for catching
// prelude-builder regressions, and `loadstring`/`load` parse without running.
//
// Requires `lua5.0` .. `lua5.5` on PATH (already true in CI via build-lua.sh).
//
// Run via: just check-playground-preludes

import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  TARGET_TO_VERSION,
  buildLualibPrelude,
  buildMiddleclassPrelude,
} from "../website/src/components/playground/lua-preludes";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(HERE, "..");

const middleclassSource = readFileSync(
  path.join(REPO_ROOT, "website/src/assets/wasm/middleclass.lua"),
  "utf8",
);

// Synthetic stand-in for the per-request lualib bundle. The committed
// internal/lualib/lualib_bundle.lua targets 5.1+, so it can't load on 5.0
// for reasons unrelated to the prelude builders themselves. We only want to
// exercise the literal encoder and prelude wrapper here, including bytes
// that would collide with naive long-bracket escaping (`]]`, `]==]`).
const fakeLualibBundle = `-- contains ]] and ]==] to stress the literal encoder
return { tag = "fake-lualib" }
`;

const tmp = mkdtempSync(path.join(tmpdir(), "tslua-prelude-check-"));
try {
  // Parse-only wrapper: load (don't execute) the chunk file passed as arg[1].
  // Using a wrapper script rather than `-e` so `arg` is populated on every
  // Lua version (5.0 doesn't populate arg for -e invocations).
  const wrapper = path.join(tmp, "parse-check.lua");
  writeFileSync(
    wrapper,
    `local f = assert(io.open(arg[1], "r"))
local s = f:read("*a")
f:close()
assert((loadstring or load)(s, "@" .. arg[1]))
`,
  );

  let failed = 0;
  const targets = Object.keys(TARGET_TO_VERSION);

  for (const target of targets) {
    const version = TARGET_TO_VERSION[target]!;
    const bin = `lua${version}`;
    const program =
      buildLualibPrelude(fakeLualibBundle, target) +
      buildMiddleclassPrelude(middleclassSource, target) +
      'print("ok")\n';
    const file = path.join(tmp, `prelude-${target.replace(/\W/g, "_")}.lua`);
    writeFileSync(file, program);

    try {
      execFileSync(bin, [wrapper, file], { stdio: ["ignore", "pipe", "pipe"] });
      console.log(`[ok]   target=${target} (${bin})`);
    } catch (e) {
      const err = e as { stderr?: Buffer | string; message: string; code?: string };
      if (err.code === "ENOENT") {
        console.error(`[skip] target=${target}: ${bin} not on PATH`);
        continue;
      }
      const stderr = err.stderr ? String(err.stderr) : err.message;
      console.error(`[FAIL] target=${target} (${bin}):\n${stderr}`);
      failed++;
    }
  }

  if (failed > 0) {
    console.error(`\n${failed} target(s) failed`);
    process.exit(1);
  }
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
