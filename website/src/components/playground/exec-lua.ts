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

function coerceExecResult(data: unknown): ExecResult {
  if (typeof data !== "object" || data === null) {
    return { output: [], error: "Invalid worker response" };
  }
  const d = data as { output?: unknown; error?: unknown };
  const output = Array.isArray(d.output) ? d.output.map((v) => String(v)) : [];
  const error = typeof d.error === "string" ? d.error : d.error == null ? null : String(d.error);
  return { output, error };
}

function getWorker(): Worker {
  if (worker) return worker;
  worker = new Worker(new URL("./lua-worker.ts", import.meta.url), { type: "module" });
  worker.addEventListener("message", (e: MessageEvent<LuaWorkerResponse>) => {
    const data = e.data as { id?: unknown; raw?: unknown; pretty?: unknown } | null | undefined;
    if (!data || typeof data.id !== "number") return;
    const entry = pending.get(data.id);
    if (entry) {
      clearTimeout(entry.timer);
      pending.delete(data.id);
      entry.resolve({
        raw: coerceExecResult(data.raw),
        pretty: coerceExecResult(data.pretty),
      });
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

export function execLua(code: string, target: string, lualib?: string): Promise<DualExecResult> {
  return new Promise((resolve) => {
    const id = nextId++;
    const timer = setTimeout(() => {
      killWorker();
    }, TIMEOUT_MS);

    pending.set(id, { resolve, timer });
    const msg: LuaWorkerRequest = { id, code, target };
    if (lualib) msg.lualib = lualib;
    getWorker().postMessage(msg);
  });
}
