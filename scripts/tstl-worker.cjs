// Worker for parallel TSTL transpilation (cache pre-warming)
// Receives {code, luaTarget, lib, languageExtensions} messages, returns {key, lua} results.
const { parentPort } = require("worker_threads");
const ts = require("typescript");
const path = require("path");
const tstl = require(path.resolve(__dirname, "../extern/tstl/dist/index"));

const langExtTypesPath = path.resolve(__dirname, "..", "extern", "tstl", "language-extensions");

parentPort.on("message", ({ key, code, luaTarget, lib, types, languageExtensions }) => {
  try {
    const allTypes = languageExtensions ? [...(types || []), langExtTypesPath] : types || [];
    const result = tstl.transpileVirtualProject(
      { "main.ts": code },
      {
        luaTarget,
        luaLibImport: tstl.LuaLibImportKind.Require,
        noHeader: true,
        target: ts.ScriptTarget.ES2017,
        lib: lib || ["lib.esnext.d.ts"],
        ...(allTypes.length > 0 ? { types: allTypes } : {}),
      },
    );
    const mainFile = result.transpiledFiles.find((f) => f.outPath === "main.lua");
    const lua = mainFile?.lua?.trimEnd() ?? "";
    parentPort.postMessage({ key, lua });
  } catch {
    parentPort.postMessage({ key, lua: "" });
  }
});
