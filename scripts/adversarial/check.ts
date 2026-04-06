#!/usr/bin/env node --require tsx/cjs
// Adversarial test harness: takes a TS snippet, runs JS eval + tslua Lua eval + TSTL Lua eval,
// reports whether outputs match. Exit code 0 = match, 1 = divergence, 2 = invalid snippet.
//
// Usage: echo 'snippet' | scripts/adversarial/check.sh
//    or: scripts/adversarial/check.sh -e 'snippet'
//    or: scripts/adversarial/check.sh snippet.ts

import { execFileSync } from "child_process";
import ts from "typescript";
import fs from "fs";
import path from "path";
import { serialize } from "../migrate/serialize.ts";
import { transpileVirtualProject, LuaTarget, LuaLibImportKind } from "../../extern/tstl/dist/index";

const MAX_CHARS = 500;
const JS_TIMEOUT_MS = 500;
const LUA_TIMEOUT_MS = 5000;
const TSLUA_TIMEOUT_MS = 10000;

// ---- Read snippet ----

let source = "";
const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  if (args[i] === "-e" && args[i + 1]) {
    source = args[++i];
  } else if (!args[i].startsWith("-") && fs.existsSync(args[i])) {
    source = fs.readFileSync(args[i], "utf-8");
  }
}
if (!source) {
  try {
    source = fs.readFileSync(0, "utf-8");
  } catch {}
}
source = source.trim();

// Category: who's wrong?
//   tslua-bug:  tslua wrong, TSTL correct
//   tstl-bug:   TSTL wrong, tslua correct
//   shared-bug: both wrong
//   match:      all agree
type Category = "tslua-bug" | "tstl-bug" | "shared-bug" | "match" | "invalid";

interface Result {
  status: Category;
  snippet: string;
  reason?: string;
  jsOutput?: string;
  tsluaLuaOutput?: string;
  tstlLuaOutput?: string;
  tsluaLua?: string;
  tstlLua?: string;
}

function fail(reason: string): never {
  const result: Result = { status: "invalid", snippet: source, reason };
  console.log(JSON.stringify(result, null, 2));
  process.exit(2);
}

if (!source) fail("empty snippet");
if (source.length > MAX_CHARS) fail(`snippet too long: ${source.length} chars (max ${MAX_CHARS})`);

// ---- Wrap as function (all snippets must produce output via console.log) ----

const fullTs = `export function __main() {\n${source}\n}`;

// ---- Step 1: TypeScript typecheck ----

const compilerOptions: ts.CompilerOptions = {
  target: ts.ScriptTarget.ES2017,
  module: ts.ModuleKind.CommonJS,
  strict: true,
  noEmit: true,
};

const host = ts.createCompilerHost(compilerOptions);
const originalGetSourceFile = host.getSourceFile;
host.getSourceFile = (fileName, languageVersion, onError) => {
  if (fileName === "snippet.ts") {
    return ts.createSourceFile(fileName, fullTs, languageVersion, true);
  }
  return originalGetSourceFile.call(host, fileName, languageVersion, onError);
};
host.fileExists = (fileName) => fileName === "snippet.ts" || ts.sys.fileExists(fileName);
host.readFile = (fileName) => (fileName === "snippet.ts" ? fullTs : ts.sys.readFile(fileName));

const program = ts.createProgram(["snippet.ts"], compilerOptions, host);
const diagnostics = ts.getPreEmitDiagnostics(program).filter((d) => {
  // Allow "cannot find name 'console'" — we don't have DOM lib
  const msg = ts.flattenDiagnosticMessageText(d.messageText, "\n");
  if (msg.includes("Cannot find name 'console'")) return false;
  return true;
});

if (diagnostics.length > 0) {
  const msgs = diagnostics.map((d) => ts.flattenDiagnosticMessageText(d.messageText, "\n"));
  fail(`TypeScript diagnostics:\n${msgs.join("\n")}`);
}

// ---- Step 1b: ESLint strict-boolean-expressions ----

try {
  const eslintTmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "tslua-eslint-"));
  const eslintTmpFile = path.join(eslintTmpDir, "snippet.ts");
  fs.writeFileSync(eslintTmpFile, fullTs);
  fs.writeFileSync(
    path.join(eslintTmpDir, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: { target: "ES2017", module: "commonjs", strict: true },
      include: ["snippet.ts"],
    }),
  );

  const eslintRealDir = fs.realpathSync(eslintTmpDir);
  const eslintRealFile = path.join(eslintRealDir, "snippet.ts");

  fs.writeFileSync(eslintRealFile, fullTs);
  fs.writeFileSync(
    path.join(eslintRealDir, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: { target: "ES2017", module: "commonjs", strict: true, lib: ["ES2017"] },
      include: ["*.ts"],
    }),
  );

  const repoRoot = path.resolve(".");
  fs.writeFileSync(
    path.join(eslintRealDir, "eslint.config.mjs"),
    `import tseslint from "${repoRoot}/node_modules/typescript-eslint/dist/index.js";\n` +
      `export default [{\n` +
      `  files: ["**/*.ts"],\n` +
      `  languageOptions: { parser: tseslint.parser, parserOptions: { project: "./tsconfig.json", tsconfigRootDir: "${eslintRealDir}" } },\n` +
      `  plugins: { "@typescript-eslint": tseslint.plugin },\n` +
      `  rules: { "@typescript-eslint/strict-boolean-expressions": ["error", { allowNumber: false, allowString: false }] },\n` +
      `}];\n`,
  );

  try {
    execFileSync(path.resolve("node_modules/.bin/eslint"), [eslintRealFile], {
      encoding: "utf-8",
      timeout: 10000,
      stdio: ["pipe", "pipe", "pipe"],
      cwd: eslintRealDir,
    });
  } catch (e: any) {
    const stdout = e.stdout?.toString().trim() || "";
    if (e.status === 1 && stdout.includes("strict-boolean-expressions")) {
      fail(`ESLint strict-boolean-expressions:\n${stdout}`);
    }
  } finally {
    fs.rmSync(eslintTmpDir, { recursive: true, force: true });
  }
} catch {
  // eslint not available — skip
}

// ---- Step 2: JS eval ----

let jsOutput: string;
try {
  const jsCode = ts.transpileModule(fullTs, {
    compilerOptions: {
      target: ts.ScriptTarget.ES2017,
      module: ts.ModuleKind.CommonJS,
      strict: true,
    },
  }).outputText;

  const vm = require("vm");
  const captured: string[] = [];
  const mockConsole = {
    log: (...logArgs: any[]) => captured.push(logArgs.map((a) => serialize(a)).join("\t")),
  };
  const mainExports: any = {};
  const mainModule = { exports: mainExports };
  const ctx = vm.createContext({
    exports: mainExports,
    module: mainModule,
    require,
    console: mockConsole,
  });

  const start = performance.now();
  vm.runInContext(jsCode, ctx, { timeout: JS_TIMEOUT_MS });
  mainModule.exports.__main();
  const elapsed = performance.now() - start;

  if (elapsed > JS_TIMEOUT_MS) fail(`JS eval too slow: ${elapsed.toFixed(0)}ms`);
  if (captured.length === 0) fail("no console.log output from JS");

  jsOutput = captured.join("\n");
} catch (e: any) {
  fail(`JS eval error: ${e.message}`);
}

// ---- Step 3: tslua transpile (no diagnostics) ----

let tsluaLua: string;
try {
  const result = execFileSync(
    "./tslua",
    ["eval", "-e", fullTs, "--luaTarget", "JIT", "--diagnosticFormat", "native"],
    {
      encoding: "utf-8",
      timeout: TSLUA_TIMEOUT_MS,
      stdio: ["pipe", "pipe", "pipe"],
    },
  );
  tsluaLua = result.trimEnd();
} catch (e: any) {
  const stderr = e.stderr?.toString().trim() || "";
  const stdout = e.stdout?.toString().trim() || "";
  if (e.status !== 0) {
    if (stderr.includes("error TSTL") || stderr.includes("error TS")) {
      fail(`tslua diagnostics:\n${stderr}`);
    }
  }
  tsluaLua = stdout || "";
  if (stderr && (stderr.includes("error TSTL") || stderr.includes("error TS"))) {
    fail(`tslua diagnostics:\n${stderr}`);
  }
}

if (!tsluaLua) fail("tslua produced no output");

if (tsluaLua.includes("--[[ unsupported:") || tsluaLua.includes("--[[ nil --[[ unsupported:")) {
  fail("tslua emitted unsupported node marker");
}

// ---- Step 4: TSTL transpile ----

let tstlLua: string | undefined;
try {
  const tstlResult = transpileVirtualProject(
    { "main.ts": fullTs },
    {
      luaTarget: LuaTarget.LuaJIT,
      luaLibImport: LuaLibImportKind.Require,
      noHeader: true,
      target: ts.ScriptTarget.ES2017,
    },
  );
  const mainFile = tstlResult.transpiledFiles.find((f) => f.outPath === "main.lua");
  tstlLua = mainFile?.lua?.trimEnd() ?? "";
  if (!tstlLua) tstlLua = undefined;
} catch {
  // TSTL failed — we'll still report tslua results
}

// ---- Step 5: Lua eval helper ----

const serializeLuaCode = fs.readFileSync(path.resolve("scripts/serialize.lua"), "utf-8");
const lualibSrc = path.resolve("internal/lualib/lualib_bundle.lua");
// TSTL has its own lualib
const tstlLualibSrc = path.resolve("extern/tstl/dist/lualib_bundle.lua");

function evalLua(lua: string, useTstlLualib: boolean): string {
  const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "tslua-adv-"));
  try {
    fs.writeFileSync(path.join(tmpDir, "main.lua"), lua);
    const libSrc = useTstlLualib ? tstlLualibSrc : lualibSrc;
    if (fs.existsSync(libSrc)) {
      fs.copyFileSync(libSrc, path.join(tmpDir, "lualib_bundle.lua"));
    }

    const runnerCode =
      serializeLuaCode +
      `\nlocal __captured = {}\n` +
      `local __orig_print = print\n` +
      `print = function(...)\n` +
      `  local args = {...}\n` +
      `  local parts = {}\n` +
      `  for i = 1, select("#", ...) do parts[#parts+1] = serialize(args[i]) end\n` +
      `  __captured[#__captured+1] = table.concat(parts, "\\t")\n` +
      `end\n` +
      `package.path = "${tmpDir}/?.lua"\n` +
      `local mod = require("main")\n` +
      `mod.__main()\n` +
      `print = __orig_print\n` +
      `io.write(table.concat(__captured, "\\n"))\n`;

    const runnerPath = path.join(tmpDir, "runner.lua");
    fs.writeFileSync(runnerPath, runnerCode);

    return execFileSync("luajit", [runnerPath], {
      encoding: "utf-8",
      timeout: LUA_TIMEOUT_MS,
      stdio: ["pipe", "pipe", "pipe"],
    }).trim();
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
}

// ---- Step 6: Eval both ----

let tsluaLuaOutput: string;
try {
  tsluaLuaOutput = evalLua(tsluaLua, false);
} catch (e: any) {
  const stderr = e.stderr?.toString().trim() || e.message || "unknown error";
  fail(`tslua Lua eval error: ${stderr}`);
}

let tstlLuaOutput: string | undefined;
if (tstlLua) {
  try {
    tstlLuaOutput = evalLua(tstlLua, true);
  } catch {
    // TSTL Lua eval failed — still report tslua results
  }
}

// ---- Step 7: Categorize ----

const tsluaCorrect = tsluaLuaOutput === jsOutput;
const tstlCorrect = tstlLuaOutput === undefined ? undefined : tstlLuaOutput === jsOutput;

let status: Category;
if (tsluaCorrect && (tstlCorrect === undefined || tstlCorrect)) {
  status = "match";
} else if (!tsluaCorrect && tstlCorrect === true) {
  status = "tslua-bug";
} else if (tsluaCorrect && tstlCorrect === false) {
  status = "tstl-bug";
} else if (!tsluaCorrect && tstlCorrect === false) {
  status = "shared-bug";
} else {
  // tstlCorrect is undefined and tsluaCorrect is false
  status = "tslua-bug";
}

const result: Result = {
  status,
  snippet: source,
  jsOutput,
  tsluaLuaOutput,
  tstlLuaOutput,
  tsluaLua,
  tstlLua,
};

console.log(JSON.stringify(result, null, 2));

// Append non-match results to JSONL files by category
if (status !== "match") {
  const logDir = path.resolve(__dirname);
  const entry = { ...result, timestamp: new Date().toISOString() };
  const line = JSON.stringify(entry) + "\n";

  // Always append to the main divergences log
  fs.appendFileSync(path.join(logDir, "divergences.jsonl"), line);
}

process.exit(status === "match" ? 0 : 1);
