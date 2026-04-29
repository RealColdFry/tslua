// Main-thread proxy for the tslua WASM transpile worker.
// All transpile work runs off-main-thread; this module only sends messages
// and resolves promises with results.

import type {
  TranspileWorkerMessage,
  TranspileWorkerResponse,
} from "./wasm-worker";

export interface WasmDiagnostic {
  startLine: number; // 1-based
  startCol: number; // 1-based, UTF-16
  endLine: number;
  endCol: number;
  message: string;
  severity: number; // Monaco MarkerSeverity: 1=Hint, 2=Info, 4=Warning, 8=Error
  code: number; // diagnostic code (e.g. 100037)
}

export interface TsluaOptions {
  compilerOptions?: Record<string, unknown>;
  extraFiles?: Record<string, string>;
  tstl?: {
    luaTarget?: string;
    luaLibImport?: string;
    emitMode?: string;
    classStyle?: string;
    noImplicitSelf?: boolean;
    noImplicitGlobalVariables?: boolean;
    trace?: boolean;
  };
}

export interface TranspileResult {
  lua: string;
  lualibBundle: string;
  errors: string[];
  diagnostics: WasmDiagnostic[];
}

let worker: Worker | null = null;
let loadPromise: Promise<void> | null = null;
let nextId = 0;
const pending = new Map<number, (r: TranspileResult) => void>();

function getWorker(): Worker {
  if (worker) return worker;
  worker = new Worker(new URL("./wasm-worker.ts", import.meta.url), { type: "module" });
  worker.addEventListener("message", (e: MessageEvent<TranspileWorkerResponse>) => {
    const msg = e.data;
    if (msg.type === "result") {
      const cb = pending.get(msg.id);
      if (cb) {
        pending.delete(msg.id);
        cb(msg.result);
      }
    }
  });
  return worker;
}

export function loadWasm(): Promise<void> {
  if (loadPromise) return loadPromise;
  loadPromise = new Promise<void>((resolve, reject) => {
    const w = getWorker();
    const onMsg = (e: MessageEvent<TranspileWorkerResponse>) => {
      const msg = e.data;
      if (msg.type === "ready") {
        w.removeEventListener("message", onMsg);
        resolve();
      } else if (msg.type === "init-error") {
        w.removeEventListener("message", onMsg);
        reject(new Error(msg.message));
      }
    };
    w.addEventListener("message", onMsg);
    w.postMessage({ type: "init" } satisfies TranspileWorkerMessage);
  });
  return loadPromise;
}

export function transpile(code: string, options: TsluaOptions): Promise<TranspileResult> {
  return new Promise((resolve) => {
    const id = nextId++;
    pending.set(id, resolve);
    getWorker().postMessage({
      type: "transpile",
      id,
      code,
      options,
    } satisfies TranspileWorkerMessage);
  });
}
