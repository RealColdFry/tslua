// Worker for parallel TSTL transpilation (cache pre-warming)
// Receives {code, luaTarget, lib} messages, returns {key, lua} results.
const { parentPort } = require("worker_threads");
const ts = require("typescript");
const path = require("path");
const tstl = require(path.resolve(__dirname, "../extern/tstl/dist/index"));

parentPort.on("message", ({ key, code, luaTarget, lib }) => {
  try {
    const result = tstl.transpileVirtualProject(
      { "main.ts": code },
      {
        luaTarget,
        luaLibImport: tstl.LuaLibImportKind.Require,
        noHeader: true,
        target: ts.ScriptTarget.ES2017,
        lib: lib || ["lib.esnext.d.ts"],
      },
    );
    const mainFile = result.transpiledFiles.find((f) => f.outPath === "main.lua");
    const lua = mainFile?.lua?.trimEnd() ?? "";
    parentPort.postMessage({ key, lua });
  } catch {
    parentPort.postMessage({ key, lua: "" });
  }
});
