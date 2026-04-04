// Main-thread wrapper for the Lua execution Web Worker.
// Handles message passing, timeouts, and worker respawning.

import type { LuaWorkerRequest, LuaWorkerResponse } from "./lua-worker";

export interface ExecResult {
  output: string[];
  error: string | null;
}

export interface DualExecResult {
  raw: ExecResult;
  pretty: ExecResult;
}

const TIMEOUT_MS = 5000;

let worker: Worker | null = null;
let nextId = 0;
const pending = new Map<
  number,
  { resolve: (r: DualExecResult) => void; timer: ReturnType<typeof setTimeout> }
>();

function getWorker(): Worker {
  if (worker) return worker;
  worker = new Worker(new URL("./lua-worker.ts", import.meta.url), { type: "module" });
  worker.addEventListener("message", (e: MessageEvent<LuaWorkerResponse>) => {
    const { id, raw, pretty } = e.data;
    const entry = pending.get(id);
    if (entry) {
      clearTimeout(entry.timer);
      pending.delete(id);
      entry.resolve({ raw, pretty });
    }
  });
  return worker;
}

function killWorker() {
  if (worker) {
    worker.terminate();
    worker = null;
  }
  // Reject all pending requests
  for (const [id, entry] of pending) {
    clearTimeout(entry.timer);
    pending.delete(id);
    const errResult: ExecResult = { output: [], error: "Execution timed out" };
    entry.resolve({ raw: errResult, pretty: errResult });
  }
}

export function execLua(code: string, target: string): Promise<DualExecResult> {
  return new Promise((resolve) => {
    const id = nextId++;
    const timer = setTimeout(() => {
      killWorker();
    }, TIMEOUT_MS);

    pending.set(id, { resolve, timer });
    getWorker().postMessage({ id, code, target } satisfies LuaWorkerRequest);
  });
}
