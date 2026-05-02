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

import { buildLualibPrelude, buildMiddleclassPrelude, TARGET_TO_VERSION } from "./lua-preludes";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBSITE_ROOT = path.resolve(HERE, "../../..");

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
      it("lualib prelude loads, runs, and exposes require('lualib_bundle')", async () => {
        const h = await newLuaState(version);
        const err = runChunk(h, buildLualibPrelude(fakeLualibBundle, target));
        expect(err, `lualib prelude failed: ${err}`).toBeNull();
        const probeErr = runChunk(h, 'print("lualib=" .. type(require("lualib_bundle")))');
        expect(probeErr).toBeNull();
        h.lua.lua_close(h.L);
        expect(h.output).toContain("lualib=table");
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
