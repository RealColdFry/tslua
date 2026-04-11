import fs from "fs";
import path from "path";
import os from "os";
import { glob } from "fs/promises";
import { Worker } from "worker_threads";
import { Command } from "commander";
import type { CacheMiss, TestCase } from "./types.ts";
import { migrateSpec } from "./migrate.ts";
import { extractTestCases } from "./extract.ts";
import {
  loadRefLuaCache,
  saveRefLuaCache,
  refLuaCache,
  refLuaCacheKey,
  setCacheEntry,
} from "./tstl-ref.ts";

const program = new Command()
  .name("migrate-tstl-test")
  .description("Extract test cases from TSTL spec files and generate Go test files")
  .argument("[spec-files...]", "TSTL spec files to migrate (default: regenerate all)")
  .option("-v, --verbose", "show skipped test names and details")
  .option("-a, --all", "scan all TSTL spec files instead of only already-migrated ones")
  .option("-c, --check", "fast path: only run extraction and report missing capabilities")
  .parse();

const opts = program.opts<{ verbose?: boolean; all?: boolean; check?: boolean }>();

function resolveSpecPath(p: string): string {
  if (!p.startsWith("test/") && !p.startsWith("extern/")) p = `test/unit/${p}`;
  if (!p.endsWith(".spec.ts")) p += ".spec.ts";
  if (!p.startsWith("extern/")) p = `extern/tstl/${p}`;
  return p;
}

async function discoverAllSpecs(): Promise<string[]> {
  const specs: string[] = [];
  for await (const entry of glob("extern/tstl/test/unit/**/*.spec.ts")) {
    specs.push(entry);
  }
  return specs.toSorted();
}

function discoverMigratedSpecs(): string[] {
  const testDir = "internal/tstltest";
  const seen = new Set<string>();
  for (const f of fs.readdirSync(testDir)) {
    if (!f.endsWith("_test.go")) continue;
    const first = fs.readFileSync(path.join(testDir, f), "utf-8").split("\n")[0];
    const m = first.match(/from (extern\/\S+\.spec\.ts)/);
    if (m) seen.add(m[1]);
  }
  return [...seen].toSorted();
}

// ---- Check mode: fast extraction-only report ----

function runCheck(specPaths: string[]): void {
  // error message → list of "spec::testName"
  const errorsByMessage = new Map<string, string[]>();
  let totalCases = 0;
  let totalErrors = 0;
  let totalSpecs = 0;
  let specsWithErrors = 0;

  for (const specPath of specPaths) {
    totalSpecs++;
    let cases: TestCase[];
    let extractionErrors: { name: string; error: string }[];
    try {
      ({ cases, extractionErrors } = extractTestCases(specPath));
    } catch (e: any) {
      cases = [];
      extractionErrors = [{ name: "(crash)", error: e.message ?? String(e) }];
    }
    // Count "other" assertion cases as extraction failures, they can't be migrated
    for (const c of cases) {
      if (c.assertion === "other") {
        extractionErrors.push({
          name: c.name,
          error: `uses .${c.otherReason ?? "unknown"}(), not migratable`,
        });
      }
    }
    totalCases += cases.filter((c) => c.assertion !== "other").length;
    if (extractionErrors.length > 0) {
      specsWithErrors++;
      totalErrors += extractionErrors.length;
    }
    for (const err of extractionErrors) {
      const list = errorsByMessage.get(err.error) ?? [];
      list.push(`${path.basename(specPath, ".spec.ts")}::${err.name}`);
      errorsByMessage.set(err.error, list);
    }
  }

  // Group error messages by root cause (strip test-specific prefixes)
  // e.g. "util.testExpression(...).getLuaExecutionResult is not a function"
  //   and "util.testFunction(...).getLuaExecutionResult is not a function"
  // both map to missing method "getLuaExecutionResult"
  const byCapability = new Map<
    string,
    { count: number; messages: Set<string>; specs: Set<string> }
  >();
  for (const [message, tests] of errorsByMessage) {
    const capability = classifyError(message);
    const entry = byCapability.get(capability) ?? {
      count: 0,
      messages: new Set(),
      specs: new Set(),
    };
    entry.count += tests.length;
    entry.messages.add(message);
    for (const t of tests) entry.specs.add(t.split("::")[0]);
    byCapability.set(capability, entry);
  }

  // Sort by count descending
  const sorted = [...byCapability.entries()].toSorted((a, b) => b[1].count - a[1].count);

  const totalFound = totalCases + totalErrors;
  const pct = totalFound > 0 ? ((totalCases / totalFound) * 100).toFixed(1) : "0";
  console.error(
    `\n${totalSpecs} specs scanned, ${totalCases} / ${totalFound} cases migrated (${pct}%), ${totalErrors} extraction failures in ${specsWithErrors} specs\n`,
  );

  if (sorted.length === 0) {
    console.error("No extraction failures.");
    return;
  }

  console.error("Missing capabilities:\n");
  for (const [capability, { count, specs }] of sorted) {
    console.error(`  ${count.toString().padStart(4)}  ${capability}  (${specs.size} specs)`);
  }
  console.error("");

  if (opts.verbose) {
    for (const [capability, { messages, count }] of sorted) {
      console.error(`--- ${capability} (${count}) ---`);
      for (const msg of messages) {
        const tests = errorsByMessage.get(msg)!;
        console.error(`  ${msg}`);
        for (const t of tests) {
          console.error(`    ${t}`);
        }
      }
      console.error("");
    }
  }
}

function classifyError(message: string): string {
  // Missing builder method: "foo.getMainLuaCodeChunk is not a function"
  const methodMatch = message.match(/\.(\w+) is not a function$/);
  if (methodMatch) return `missing method: ${methodMatch[1]}`;

  // Missing property: "Cannot read properties of undefined (reading 'ESNext')"
  const propMatch = message.match(/Cannot read properties of undefined \(reading '(\w+)'\)/);
  if (propMatch) return `missing property: ${propMatch[1]}`;

  // Missing constructor: "util.ExecutionError is not a constructor"
  const ctorMatch = message.match(/(\w+(?:\.\w+)*) is not a constructor$/);
  if (ctorMatch) return `missing constructor: ${ctorMatch[1]}`;

  // Missing global: "__dirname is not defined"
  const globalMatch = message.match(/^(\w+) is not defined$/);
  if (globalMatch) return `missing global: ${globalMatch[1]}`;

  return `other: ${message}`;
}

// ---- Main ----

(async () => {
  let specPaths = program.args.map(resolveSpecPath);

  if (specPaths.length === 0) {
    if (opts.all) {
      specPaths = await discoverAllSpecs();
      console.error(`Discovered ${specPaths.length} TSTL spec files.\n`);
    } else {
      specPaths = discoverMigratedSpecs();
      if (specPaths.length === 0) {
        console.error("No generated test files found and no spec files given.");
        process.exit(1);
      }
      console.error(`Regenerating ${specPaths.length} existing test files...\n`);
    }
  }

  // Fast path: extraction-only check
  if (opts.check) {
    runCheck(specPaths);
    return;
  }

  // Normal migration path
  loadRefLuaCache();

  const isTTY = process.stderr.isTTY ?? false;

  // Collect all test cases first to find cache misses for parallel pre-warming
  const allCasesPerSpec: Map<string, TestCase[]> = new Map();
  for (const specPath of specPaths) {
    const { cases } = extractTestCases(specPath);
    allCasesPerSpec.set(specPath, cases);
  }

  // Find cache misses that need TSTL transpilation
  const cacheMisses: CacheMiss[] = [];
  for (const [, cases] of allCasesPerSpec) {
    for (const tc of cases) {
      const header = tc.tsHeader ? tc.tsHeader + "\n" : "";
      let fullCode: string;
      switch (tc.mode) {
        case "expression":
          fullCode = `${header}export const __result = ${tc.tsCode};`;
          break;
        case "function":
          fullCode = `${header}export function __main() {${tc.tsCode}}`;
          break;
        case "module":
          fullCode = header + tc.tsCode;
          break;
        default:
          continue;
      }
      const luaTarget = (tc.options?.luaTarget as string) ?? "5.5";
      const lib = tc.options?.lib as string[] | undefined;
      const types = tc.options?.types as string[] | undefined;
      const languageExtensions = tc.languageExtensions === true;
      const key = refLuaCacheKey(fullCode!, luaTarget, lib, languageExtensions);
      if (!(key in refLuaCache)) {
        cacheMisses.push({ key, code: fullCode!, luaTarget, lib, types, languageExtensions });
      }
    }
  }

  // Pre-warm cache in parallel using worker threads
  async function prewarmCache(missList: CacheMiss[]) {
    const numWorkers = Math.min(os.cpus().length, missList.length, 8);

    process.stderr.write(
      `Pre-warming ${missList.length} TSTL cache entries using ${numWorkers} workers...\n`,
    );

    await new Promise<void>((resolve) => {
      const workers: Worker[] = [];
      let nextIdx = 0;
      let doneCount = 0;
      let lastProgress = "";

      function showCacheProgress() {
        if (isTTY) {
          const pct = Math.round((doneCount / missList.length) * 100);
          const msg = `\r  [${doneCount}/${missList.length} ${pct}%]`;
          const padded = msg + " ".repeat(Math.max(0, lastProgress.length - msg.length));
          process.stderr.write(padded);
          lastProgress = msg;
        } else if (doneCount % 50 === 0 || doneCount === missList.length) {
          process.stderr.write(`  [${doneCount}/${missList.length}]\n`);
        }
      }

      function feedWorker(w: Worker) {
        if (nextIdx < missList.length) {
          w.postMessage(missList[nextIdx++]); // eslint-disable-line unicorn/require-post-message-target-origin
        }
      }

      for (let i = 0; i < numWorkers; i++) {
        const w = new Worker(path.resolve(__dirname, "../tstl-worker.cjs"));
        w.on("message", ({ key, lua }: { key: string; lua: string }) => {
          setCacheEntry(key, lua);
          doneCount++;
          showCacheProgress();
          feedWorker(w);
          if (doneCount === missList.length) {
            for (const w2 of workers) w2.terminate();
            if (isTTY) process.stderr.write("\n");
            resolve();
          }
        });
        workers.push(w);
        feedWorker(w);
      }
    });

    // Save cache after pre-warming so progress isn't lost
    saveRefLuaCache();
  }

  if (cacheMisses.length > 0) {
    const uniqueMisses = new Map<string, CacheMiss>();
    for (const m of cacheMisses) {
      uniqueMisses.set(m.key, m);
    }
    await prewarmCache([...uniqueMisses.values()]);
  }

  // Now run migration (all TSTL refs are cached, so this is fast)
  const total = specPaths.length;
  let completed = 0;
  let lastProgressLine = "";
  let totalCasesGenerated = 0;
  let totalErrors = 0;

  for (const specPath of specPaths) {
    const result = migrateSpec(specPath);
    completed++;

    const hasErrors = result.bakeErrors.length > 0 || result.extractionErrors.length > 0;
    totalErrors += result.bakeErrors.length + result.extractionErrors.length;

    // Extract case count from summary (first part is always "N cases")
    const caseMatch = result.summary.match(/^(\d+) cases/);
    if (caseMatch) totalCasesGenerated += Number(caseMatch[1]);

    if (isTTY) {
      // In TTY mode: clean single-line progress, errors printed above it
      if (hasErrors || opts.verbose) {
        // Clear the progress line, print detail, then redraw progress
        if (lastProgressLine) process.stderr.write(`\r${" ".repeat(lastProgressLine.length)}\r`);
        process.stderr.write(`${path.basename(specPath, ".spec.ts")} (${result.summary})\n`);
        if (opts.verbose) {
          for (const s of result.skippedCases) {
            process.stderr.write(`  SKIPPED: ${s.name} (${s.reason})\n`);
          }
        }
        for (const err of result.bakeErrors) {
          process.stderr.write(`  BAKE FAILED: ${err}\n`);
        }
        for (const err of result.extractionErrors) {
          process.stderr.write(`  EXTRACT FAILED: ${err.name}: ${err.error}\n`);
        }
      }
      const pct = Math.round((completed / total) * 100);
      const msg = `[${completed}/${total} ${pct}%] ${path.basename(specPath, ".spec.ts")}`;
      const padded = `\r${msg}${" ".repeat(Math.max(0, lastProgressLine.length - msg.length))}`;
      process.stderr.write(padded);
      lastProgressLine = msg;
    } else {
      // Non-TTY: one line per spec
      process.stderr.write(`${path.basename(specPath, ".spec.ts")} (${result.summary})\n`);
      for (const err of result.bakeErrors) {
        process.stderr.write(`  BAKE FAILED: ${err}\n`);
      }
      for (const err of result.extractionErrors) {
        process.stderr.write(`  EXTRACT FAILED: ${err.name}: ${err.error}\n`);
      }
    }
  }
  if (isTTY) {
    process.stderr.write(`\r${" ".repeat(lastProgressLine.length)}\r`);
  }
  process.stderr.write(`Done: ${total} specs, ${totalCasesGenerated} cases generated`);
  if (totalErrors > 0) process.stderr.write(`, ${totalErrors} errors`);
  process.stderr.write("\n");
  saveRefLuaCache();
})();
