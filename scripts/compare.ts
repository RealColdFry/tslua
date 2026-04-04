#!/usr/bin/env node --require tsx/cjs
// Compare tool: side-by-side view of TS → JS eval, tslua Lua, TSTL Lua, and Lua eval.

import { execFileSync } from "child_process";
import ts from "typescript";
import fs from "fs";
import path from "path";
import { Command } from "commander";
import { transpileVirtualProject, LuaTarget, LuaLibImportKind } from "../extern/tstl/dist/index";
import { serialize } from "./migrate/serialize.ts";

// ---- Target mapping ----

const tstlTargetMap: Record<string, LuaTarget> = {
  JIT: LuaTarget.LuaJIT,
  "5.0": LuaTarget.Lua50,
  "5.1": LuaTarget.Lua51,
  "5.2": LuaTarget.Lua52,
  "5.3": LuaTarget.Lua53,
  "5.4": LuaTarget.Lua54,
  "5.5": (LuaTarget as any).Lua55 ?? LuaTarget.Lua54,
  universal: LuaTarget.Universal,
};

const validTargets = Object.keys(tstlTargetMap);

// ---- CLI ----

const program = new Command()
  .name("compare")
  .description("Side-by-side: TS AST, JS eval, tslua Lua, TSTL Lua, diff, Lua eval")
  .argument("[code]", "TypeScript source (alternative to -e)")
  .option("-e, --expr <code>", "TypeScript source")
  .option(
    "-t, --target <targets>",
    `Lua target(s), comma-separated (${validTargets.join(", ")})`,
    "JIT",
  )
  .option("-m, --mode <mode>", "expression | function | module (default: auto)", "auto")
  .option("--ast", "show AST output (off by default)")
  .option("--no-eval", "skip JS/Lua evaluation")
  .option("--no-diff", "skip diff")
  .option("--trace", "add --[[trace]] comments to tslua output")
  .addHelpText(
    "after",
    `
Examples:
  just compare -e '1 + 2'
  just compare -e 'Math.atan2(4, 5)' -t 5.3,5.4
  just compare -e 'function f(x: number) { return x * 2 } f(21)'
  echo 'const x = [1,2,3]; x.map(v => v*2)' | just compare
  just compare -e '[...[1,2,3]]' -t JIT,universal --no-eval`,
  )
  .parse();

const opts = program.opts<{
  expr?: string;
  target: string;
  mode: string;
  ast: boolean;
  eval: boolean;
  diff: boolean;
  trace: boolean;
}>();

let source = opts.expr ?? program.args[0] ?? "";
if (!source) {
  try {
    source = fs.readFileSync(0, "utf-8").trim();
  } catch {}
}
if (!source) {
  program.help();
}

const targets = opts.target.split(",").map((t) => t.trim());
for (const t of targets) {
  if (!tstlTargetMap[t]) {
    console.error(`Unknown target: ${t}\nValid targets: ${validTargets.join(", ")}`);
    process.exit(1);
  }
}

const showAst = opts.ast;
const showEval = opts.eval;
const showDiff = opts.diff;
const showTrace = opts.trace;

type Mode = "expression" | "function" | "module";
const resolvedMode: Mode = opts.mode === "auto" ? detectMode(source) : (opts.mode as Mode);

// ---- Mode detection ----

function detectMode(code: string): Mode {
  const trimmed = code.trim();
  if (/^(import |export |class )/.test(trimmed)) return "module";
  if (/^(const |let |var |function |for |while |if |switch |try )/.test(trimmed)) return "function";
  if (trimmed.includes(";") || trimmed.includes("\n")) return "function";
  return "expression";
}

// ---- Wrap code for each mode ----

function wrapForTslua(code: string, m: Mode): string {
  switch (m) {
    case "expression":
      return `export const __result = ${code};`;
    case "function":
      return `export function __main() {${code}}`;
    case "module":
      return code;
  }
}

function luaAccessor(m: Mode): string {
  switch (m) {
    case "expression":
      return "mod.__result";
    case "function":
      return "mod.__main()";
    case "module":
      return "mod";
  }
}

// ---- Colors ----

const C = {
  reset: "\x1b[0m",
  bold: "\x1b[1m",
  dim: "\x1b[2m",
  red: "\x1b[31m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  blue: "\x1b[34m",
  magenta: "\x1b[35m",
  cyan: "\x1b[36m",
};

function header(label: string, color: string = C.cyan) {
  console.log(
    `\n${color}${C.bold}── ${label} ${"─".repeat(Math.max(0, 60 - label.length))}${C.reset}`,
  );
}

function numbered(code: string): string {
  const lines = code.split("\n");
  const w = String(lines.length).length;
  return lines.map((l, i) => `${C.dim}${String(i + 1).padStart(w)}│${C.reset} ${l}`).join("\n");
}

function stripTrace(s: string): string {
  return s.replace(/^[ \t]*--\[\[trace: .*?\]\]\n/gm, "");
}

// ---- Run for each target ----

const fullTs = wrapForTslua(source, resolvedMode);

// Show input + AST once (shared across targets)
header(`TypeScript (mode: ${resolvedMode})`, C.magenta);
console.log(numbered(fullTs));

if (showAst) {
  header("AST", C.blue);
  try {
    const ast = execFileSync("./tslua", ["ast", "-e", source], {
      encoding: "utf-8",
      timeout: 10000,
    }).trim();
    console.log(ast);
  } catch (e: any) {
    console.log(`${C.red}(tslua ast failed: ${e.message?.split("\n")[0]})${C.reset}`);
  }
}

// JS eval once (shared across targets)
let jsResult: string | undefined;
if (showEval) {
  header("Node.js eval", C.yellow);
  try {
    const compilerOptions: ts.CompilerOptions = {
      target: ts.ScriptTarget.ES2017,
      module: ts.ModuleKind.CommonJS,
      strict: true,
    };
    const jsCode = ts.transpileModule(fullTs, { compilerOptions }).outputText;
    const vm = require("vm");
    const mainExports: any = {};
    const mainModule = { exports: mainExports };
    const ctx = vm.createContext({ exports: mainExports, module: mainModule, require, console });
    vm.runInContext(jsCode, ctx);

    let result: unknown;
    switch (resolvedMode) {
      case "expression":
        result = mainModule.exports.__result;
        break;
      case "function":
        result = mainModule.exports.__main();
        break;
      case "module":
        result = mainModule.exports;
        break;
    }
    jsResult = serialize(result);
    console.log(`${C.green}${jsResult}${C.reset}`);
  } catch (e: any) {
    jsResult = `Error: ${e.message}`;
    console.log(`${C.red}${jsResult}${C.reset}`);
  }
}

// Per-target comparison
for (const luaTarget of targets) {
  const targetLabel = targets.length > 1 ? ` [${luaTarget}]` : "";

  // tslua Lua
  header(`tslua Lua${targetLabel}`, C.cyan);
  let tsluaLua: string | undefined;
  try {
    const evalArgs = ["eval", "-e", fullTs, "--luaTarget", luaTarget];
    if (showTrace) evalArgs.push("--trace");
    tsluaLua = execFileSync("./tslua", evalArgs, {
      encoding: "utf-8",
      timeout: 10000,
    }).trimEnd();
    console.log(numbered(tsluaLua));
  } catch (e: any) {
    const stderr = e.stderr?.toString() || "";
    console.log(`${C.red}(tslua failed)${C.reset}`);
    if (stderr) console.log(stderr.trim());
  }

  // TSTL Lua
  header(`TSTL Lua${targetLabel}`, C.cyan);
  let tstlLua: string | undefined;
  try {
    const result = transpileVirtualProject(
      { "main.ts": fullTs },
      {
        luaTarget: tstlTargetMap[luaTarget] ?? LuaTarget.LuaJIT,
        luaLibImport: LuaLibImportKind.Require,
        noHeader: true,
        target: ts.ScriptTarget.ES2017,
      },
    );
    const mainFile = result.transpiledFiles.find((f) => f.outPath === "main.lua");
    tstlLua = mainFile?.lua?.trimEnd() ?? "";
    if (result.diagnostics?.length) {
      for (const d of result.diagnostics) {
        console.log(`${C.yellow}diag: ${d.messageText}${C.reset}`);
      }
    }
    console.log(numbered(tstlLua));
  } catch (e: any) {
    console.log(`${C.red}(TSTL failed: ${e.message?.split("\n")[0]})${C.reset}`);
  }

  // Diff
  if (showDiff && tsluaLua !== undefined && tstlLua !== undefined) {
    const tsluaForDiff = stripTrace(tsluaLua);
    const tstlForDiff = stripTrace(tstlLua);
    if (tsluaForDiff === tstlForDiff) {
      header(`Diff${targetLabel}`, C.green);
      console.log(`${C.green}(identical)${C.reset}`);
    } else {
      header(`Diff (tslua vs TSTL)${targetLabel}`, C.red);
      const aLines = tstlForDiff.split("\n");
      const bLines = tsluaForDiff.split("\n");
      const n = aLines.length,
        m = bLines.length;
      const dp: number[][] = Array.from({ length: n + 1 }, () =>
        Array.from<number>({ length: m + 1 }).fill(0),
      );
      for (let i = n - 1; i >= 0; i--) {
        for (let j = m - 1; j >= 0; j--) {
          if (aLines[i] === bLines[j]) dp[i][j] = dp[i + 1][j + 1] + 1;
          else dp[i][j] = Math.max(dp[i + 1][j], dp[i][j + 1]);
        }
      }
      let i = 0,
        j = 0;
      while (i < n || j < m) {
        if (i < n && j < m && aLines[i] === bLines[j]) {
          console.log(`${C.dim} ${aLines[i]}${C.reset}`);
          i++;
          j++;
        } else if (i < n && (j >= m || dp[i + 1][j] >= dp[i][j + 1])) {
          console.log(`${C.red}-${aLines[i]}${C.reset}`);
          i++;
        } else {
          console.log(`${C.green}+${bLines[j]}${C.reset}`);
          j++;
        }
      }
    }
  }

  // Lua eval
  if (showEval && (tsluaLua || tstlLua)) {
    const luaRuntime = luaTarget === "JIT" ? "luajit" : `lua${luaTarget}`;

    const serializeLuaPath = path.resolve("scripts/serialize.lua");
    let serializeFn: string;
    if (fs.existsSync(serializeLuaPath)) {
      serializeFn = fs.readFileSync(serializeLuaPath, "utf-8");
    } else {
      console.log(`${C.red}(missing scripts/serialize.lua — skipping Lua eval)${C.reset}`);
      continue;
    }

    function evalLua(lua: string): string | undefined {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "tslua-compare-"));
      try {
        fs.writeFileSync(path.join(tmpDir, "main.lua"), lua);
        const lualibSrc = path.resolve("internal/lualib/lualib_bundle.lua");
        if (fs.existsSync(lualibSrc)) {
          fs.copyFileSync(lualibSrc, path.join(tmpDir, "lualib_bundle.lua"));
        }

        const accessor = luaAccessor(resolvedMode);
        const runnerCode =
          serializeFn +
          `package.path = "${tmpDir}/?.lua"\n` +
          `local mod = require("main")\n` +
          `local result = ${accessor}\n` +
          `io.write(serialize(result))\n`;

        const runnerPath = path.join(tmpDir, "runner.lua");
        fs.writeFileSync(runnerPath, runnerCode);

        const result = execFileSync(luaRuntime, [runnerPath], {
          encoding: "utf-8",
          timeout: 10000,
          stdio: ["pipe", "pipe", "pipe"],
        }).trim();
        return result;
      } catch (e: any) {
        const stderr = e.stderr?.toString().trim() || e.message?.split("\n")[0] || "unknown error";
        return `Error: ${stderr}`;
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    }

    if (tsluaLua) {
      header(`tslua Lua eval${targetLabel}`, C.yellow);
      const result = evalLua(tsluaLua);
      if (result !== undefined) {
        const color = jsResult && result === jsResult ? C.green : C.red;
        console.log(`${color}${result}${C.reset}`);
        if (jsResult && result !== jsResult) {
          console.log(`${C.dim}(JS was: ${jsResult})${C.reset}`);
        }
      }
    }

    if (tstlLua) {
      header(`TSTL Lua eval${targetLabel}`, C.yellow);
      const result = evalLua(tstlLua);
      if (result !== undefined) {
        const color = jsResult && result === jsResult ? C.green : C.red;
        console.log(`${color}${result}${C.reset}`);
        if (jsResult && result !== jsResult) {
          console.log(`${C.dim}(JS was: ${jsResult})${C.reset}`);
        }
      }
    }
  }
}

console.log();
