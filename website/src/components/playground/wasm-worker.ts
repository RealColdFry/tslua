// Web Worker that hosts the tslua WASM module and serves transpile requests.
// Keeps the main thread free of synchronous WASM work so keystrokes never block.

import wasmUrl from "../../assets/wasm/tslua.wasm?url";
import wasmExecUrl from "../../assets/wasm/wasm_exec.js?url";
import type { TranspileResult, TsluaOptions, WasmDiagnostic } from "./wasm";

declare class Go {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): void;
}

export interface TranspileWorkerInit {
  type: "init";
}

export interface TranspileWorkerRequest {
  type: "transpile";
  id: number;
  code: string;
  options: TsluaOptions;
}

export type TranspileWorkerMessage = TranspileWorkerInit | TranspileWorkerRequest;

export interface TranspileWorkerReady {
  type: "ready";
}

export interface TranspileWorkerInitError {
  type: "init-error";
  message: string;
}

export interface TranspileWorkerResult {
  type: "result";
  id: number;
  result: TranspileResult;
}

export type TranspileWorkerResponse =
  | TranspileWorkerReady
  | TranspileWorkerInitError
  | TranspileWorkerResult;

let initPromise: Promise<void> | null = null;

function ensureInit(): Promise<void> {
  if (initPromise) return initPromise;
  initPromise = (async () => {
    const execText = await fetch(wasmExecUrl).then((r) => r.text());
    // wasm_exec.js is a classic IIFE that mutates globalThis with `Go`. Module
    // workers don't have importScripts, so eval the source to install it.
    (0, eval)(execText);
    const go = new (globalThis as unknown as { Go: new () => Go }).Go();
    const result = await WebAssembly.instantiateStreaming(fetch(wasmUrl), go.importObject);
    go.run(result.instance);
  })();
  return initPromise;
}

function isArrayLike(v: unknown): v is ArrayLike<unknown> {
  return (
    v != null && typeof v === "object" && typeof (v as { length?: unknown }).length === "number"
  );
}

function runTranspile(code: string, options: TsluaOptions): TranspileResult {
  const fn = (globalThis as unknown as { tslua_transpile?: typeof tslua_transpile })
    .tslua_transpile;
  if (typeof fn !== "function") {
    return { lua: "", lualibBundle: "", errors: ["WASM not loaded"], diagnostics: [] };
  }
  const raw = fn(code, JSON.stringify(options)) as
    | { lua?: unknown; lualibBundle?: unknown; errors?: unknown; diagnostics?: unknown }
    | null
    | undefined;
  if (!raw || typeof raw !== "object") {
    return {
      lua: "",
      lualibBundle: "",
      errors: ["WASM returned invalid result"],
      diagnostics: [],
    };
  }
  const lua = typeof raw.lua === "string" ? raw.lua : "";
  const lualibBundle = typeof raw.lualibBundle === "string" ? raw.lualibBundle : "";
  const errors = isArrayLike(raw.errors)
    ? Array.from(raw.errors as ArrayLike<unknown>, (v) => String(v))
    : [];
  const diagnostics = isArrayLike(raw.diagnostics)
    ? Array.from(raw.diagnostics as ArrayLike<WasmDiagnostic>)
    : [];
  return { lua, lualibBundle, errors, diagnostics };
}

declare function tslua_transpile(code: string, optionsJson: string): unknown;

self.addEventListener("message", async (e: MessageEvent<TranspileWorkerMessage>) => {
  const msg = e.data;
  if (msg.type === "init") {
    try {
      await ensureInit();
      self.postMessage({ type: "ready" } satisfies TranspileWorkerResponse);
    } catch (err) {
      self.postMessage({
        type: "init-error",
        message: err instanceof Error ? err.message : String(err),
      } satisfies TranspileWorkerResponse);
    }
    return;
  }
  if (msg.type === "transpile") {
    try {
      await ensureInit();
      const result = runTranspile(msg.code, msg.options);
      self.postMessage({ type: "result", id: msg.id, result } satisfies TranspileWorkerResponse);
    } catch (err) {
      self.postMessage({
        type: "result",
        id: msg.id,
        result: {
          lua: "",
          lualibBundle: "",
          errors: [err instanceof Error ? err.message : String(err)],
          diagnostics: [],
        },
      } satisfies TranspileWorkerResponse);
    }
  }
});
