// End-to-end check that the playground worker's setup chunks load and run on
// every supported Lua target, using the same `lua-wasm-bindings` build the
// playground itself uses. Catches syntax and runtime regressions in the
// prelude builders (e.g. leveled long brackets `[=[...]=]` only valid on 5.1+,
// or stdlib differences between Lua versions) before they reach the playground.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// @ts-expect-error binding-factory has no .d.ts upstream
import { createLua, createLauxLib, createLuaLib } from "lua-wasm-bindings/dist/binding-factory.js";

import {
  buildLualibPrelude,
  buildMiddleclassPrelude,
  getPrettyPrintPreamble,
  TARGET_TO_VERSION,
} from "./lua-preludes";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBSITE_ROOT = path.resolve(HERE, "../../..");
const REPO_ROOT = path.resolve(WEBSITE_ROOT, "..");

// Read middleclass directly from the committed source rather than the copy
// at website/src/assets/wasm/middleclass.lua — that copy is produced by
// `just wasm` and isn't present in CI's lint job.
const middleclassSource = readFileSync(
  path.join(REPO_ROOT, "internal/lualib/middleclass/middleclass.lua"),
  "utf8",
);

// The real, committed lualib bundles the WASM transpiler ships. 5.0 has its
// own bundle with 5.0-compatible code (uses `arg` table, table.getn, etc.);
// 5.1+ all share the same bundle. Mirrors internal/lualib/lualib.go's
// BundleForTarget.
function realLualibBundleFor(version: string): string {
  const file = version === "5.0" ? "lualib_bundle_50.lua" : "lualib_bundle.lua";
  return readFileSync(path.join(REPO_ROOT, "internal/lualib", file), "utf8");
}

// Synthetic stand-in used for one targeted edge-case test: a bundle that
// contains `]]` and `]==]` to stress the literal encoder's bracket-collision
// avoidance. The real bundles don't necessarily hit those collisions.
const trickyLualibBundle = `-- contains ]] and ]==] to stress the literal encoder
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

interface LuaHandles {
  L: number;
  output: string[];
  lua: { lua_tostring(L: number, idx: number): string; lua_close(L: number): void };
  lauxlib: { luaL_dostring(L: number, code: string): number };
}

async function newLuaState(version: string): Promise<LuaHandles> {
  const entry = VERSIONS[version];
  if (!entry) throw new Error(`no WASM for version ${version}`);

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

  const lua = createLua(glue, entry.semver) as LuaHandles["lua"];
  const lauxlib = createLauxLib(glue, lua, entry.semver) as {
    luaL_newstate(): number;
    luaL_dostring(L: number, code: string): number;
  };
  const lualib = createLuaLib(glue, entry.semver) as { luaL_openlibs(L: number): void };

  const L = lauxlib.luaL_newstate();
  lualib.luaL_openlibs(L);
  return { L, output, lua, lauxlib };
}

function runChunk(h: LuaHandles, chunk: string): string | null {
  const rc = h.lauxlib.luaL_dostring(h.L, chunk);
  if (rc !== 0) return h.lua.lua_tostring(h.L, -1);
  return null;
}

describe("playground preludes", () => {
  for (const [target, version] of Object.entries(TARGET_TO_VERSION)) {
    describe(`target=${target} (Lua ${version})`, () => {
      it("lualib prelude with the real bundle loads on this target", async () => {
        const h = await newLuaState(version);
        const err = runChunk(h, buildLualibPrelude(realLualibBundleFor(version), target));
        expect(err, `real lualib prelude failed: ${err}`).toBeNull();
        const probeErr = runChunk(h, 'print("lualib=" .. type(require("lualib_bundle")))');
        expect(probeErr).toBeNull();
        h.lua.lua_close(h.L);
        expect(h.output).toContain("lualib=table");
      });

      it("lualib prelude handles bundles with `]]` and `]==]` (literal-encoder edge case)", async () => {
        const h = await newLuaState(version);
        const err = runChunk(h, buildLualibPrelude(trickyLualibBundle, target));
        expect(err, `tricky-bundle prelude failed: ${err}`).toBeNull();
        h.lua.lua_close(h.L);
      });

      it("pretty-print preamble loads and forwards console.log varargs", async () => {
        const h = await newLuaState(version);
        const err = runChunk(h, getPrettyPrintPreamble(target));
        expect(err, `pretty preamble failed: ${err}`).toBeNull();
        // console.log on the playground is invoked as a method (`console.log(a, b)`
        // becomes `console:log(a, b)` after transpile), so the first arg `_` is
        // the table itself. Forwarding the rest must work on every target.
        const probeErr = runChunk(h, 'console:log("hello", "world")');
        expect(probeErr, `console:log failed: ${probeErr}`).toBeNull();
        h.lua.lua_close(h.L);
        expect(h.output).toContain("hello world");
      });

      // middleclass.lua uses 5.1+ `...` varargs, so the worker skips its
      // prelude on 5.0. This test asserts that policy: the prelude must load
      // successfully on 5.1+ and (intentionally) fail on 5.0.
      if (version === "5.0") {
        it("middleclass prelude is unsupported on Lua 5.0", async () => {
          const h = await newLuaState(version);
          const err = runChunk(h, buildMiddleclassPrelude(middleclassSource, target));
          h.lua.lua_close(h.L);
          expect(err, "expected middleclass prelude to fail on 5.0").not.toBeNull();
        });
      } else {
        it("middleclass prelude loads, runs, and exposes require('middleclass')", async () => {
          const h = await newLuaState(version);
          const err = runChunk(h, buildMiddleclassPrelude(middleclassSource, target));
          expect(err, `middleclass prelude failed: ${err}`).toBeNull();
          const probeErr = runChunk(h, 'print("middleclass=" .. type(require("middleclass")))');
          expect(probeErr).toBeNull();
          h.lua.lua_close(h.L);
          expect(h.output).toContain("middleclass=table");
        });
      }
    });
  }
});
