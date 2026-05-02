// End-to-end check that the playground worker's setup chunks load and run on
// every supported Lua target, using the same `lua-wasm-bindings` build the
// playground itself uses. Catches syntax and runtime regressions in the
// prelude builders (e.g. leveled long brackets `[=[...]=]` only valid on 5.1+)
// before they reach the playground.
//
// Runs in plain Node, no test framework. Exits non-zero on any failure.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// @ts-expect-error binding-factory has no .d.ts upstream
import { createLua, createLauxLib, createLuaLib } from "lua-wasm-bindings/dist/binding-factory.js";

import {
  TARGET_TO_VERSION,
  buildLualibPrelude,
  buildMiddleclassPrelude,
} from "../src/components/playground/lua-preludes";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBSITE_ROOT = path.resolve(HERE, "..");

const middleclassSource = readFileSync(
  path.join(WEBSITE_ROOT, "src/assets/wasm/middleclass.lua"),
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

interface VersionEntry {
  semver: string;
  glueModule: string;
  wasmFile: string;
}

const VERSIONS: Record<string, VersionEntry> = {
  "5.0": {
    semver: "5.0.3",
    glueModule: "lua-wasm-bindings/dist/glue/glue-lua-5.0.3.js",
    wasmFile: "glue-lua-5.0.3.wasm",
  },
  "5.1": {
    semver: "5.1.5",
    glueModule: "lua-wasm-bindings/dist/glue/glue-lua-5.1.5.js",
    wasmFile: "glue-lua-5.1.5.wasm",
  },
  "5.2": {
    semver: "5.2.4",
    glueModule: "lua-wasm-bindings/dist/glue/glue-lua-5.2.4.js",
    wasmFile: "glue-lua-5.2.4.wasm",
  },
  "5.3": {
    semver: "5.3.6",
    glueModule: "lua-wasm-bindings/dist/glue/glue-lua-5.3.6.js",
    wasmFile: "glue-lua-5.3.6.wasm",
  },
  "5.4": {
    semver: "5.4.7",
    glueModule: "lua-wasm-bindings/dist/glue/glue-lua-5.4.7.js",
    wasmFile: "glue-lua-5.4.7.wasm",
  },
  "5.5": {
    semver: "5.5.0",
    glueModule: "lua-wasm-bindings/dist/glue/glue-lua-5.5.0.js",
    wasmFile: "glue-lua-5.5.0.wasm",
  },
};

const WASM_DIR = path.join(WEBSITE_ROOT, "node_modules/lua-wasm-bindings/dist/glue");

interface RunResult {
  ok: boolean;
  output: string[];
  error?: string;
}

async function runOnTarget(target: string): Promise<RunResult> {
  const version = TARGET_TO_VERSION[target];
  if (!version) return { ok: false, output: [], error: `unknown target ${target}` };
  const entry = VERSIONS[version];
  if (!entry) return { ok: false, output: [], error: `no WASM for version ${version}` };

  const wasmBinary = new Uint8Array(readFileSync(path.join(WASM_DIR, entry.wasmFile)));
  const factoryMod = (await import(entry.glueModule)) as {
    default?: (opts: unknown) => Promise<unknown>;
  };
  const factory =
    factoryMod.default ?? (factoryMod as unknown as (opts: unknown) => Promise<unknown>);

  const output: string[] = [];
  const glue = (await factory({
    wasmBinary,
    print: (t: string) => output.push(t),
    printErr: (t: string) => output.push("[stderr] " + t),
  })) as object;

  const lua = createLua(glue, entry.semver) as {
    lua_tostring(L: number, idx: number): string;
    lua_close(L: number): void;
  };
  const lauxlib = createLauxLib(glue, lua, entry.semver) as {
    luaL_newstate(): number;
    luaL_dostring(L: number, code: string): number;
  };
  const lualib = createLuaLib(glue, entry.semver) as { luaL_openlibs(L: number): void };

  const L = lauxlib.luaL_newstate();
  lualib.luaL_openlibs(L);

  // Match the worker: skip middleclass on 5.0 (it uses `...` varargs).
  const chunks = [buildLualibPrelude(fakeLualibBundle, target)];
  if (version !== "5.0") {
    chunks.push(buildMiddleclassPrelude(middleclassSource, target));
  }
  // Probe: hit the lualib hook and (where applicable) the middleclass module.
  const probe =
    version === "5.0"
      ? 'print("ok"); print("lualib=" .. type(require("lualib_bundle")))'
      : 'print("ok"); print("lualib=" .. type(require("lualib_bundle"))); print("middleclass=" .. type(require("middleclass")))';
  chunks.push(probe);

  for (const [i, chunk] of chunks.entries()) {
    const rc = lauxlib.luaL_dostring(L, chunk);
    if (rc !== 0) {
      const err = lua.lua_tostring(L, -1);
      lua.lua_close(L);
      return { ok: false, output, error: `chunk ${i}: ${err}` };
    }
  }
  lua.lua_close(L);

  const expected = ["ok", "lualib=table"];
  if (version !== "5.0") expected.push("middleclass=table");
  for (const want of expected) {
    if (!output.some((l) => l === want)) {
      return { ok: false, output, error: `missing expected output line: ${want}` };
    }
  }
  return { ok: true, output };
}

let failed = 0;
for (const target of Object.keys(TARGET_TO_VERSION)) {
  const result = await runOnTarget(target);
  if (result.ok) {
    console.log(`[ok]   target=${target}`);
  } else {
    console.error(`[FAIL] target=${target}: ${result.error}`);
    for (const line of result.output) console.error(`         | ${line}`);
    failed++;
  }
}

if (failed > 0) {
  console.error(`\n${failed} target(s) failed`);
  process.exit(1);
}
