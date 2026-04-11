import wasmUrl from "../../assets/wasm/tslua.wasm?url";
import wasmExecUrl from "../../assets/wasm/wasm_exec.js?url";

declare class Go {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): void;
}

declare function tslua_transpile(code: string, optionsJson: string): unknown;

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
    emitMode?: string;
    classStyle?: string;
    noImplicitSelf?: boolean;
    noImplicitGlobalVariables?: boolean;
    trace?: boolean;
  };
}

let ready = false;
let initPromise: Promise<void> | null = null;

export function loadWasm(): Promise<void> {
  if (initPromise) return initPromise;
  initPromise = (async () => {
    // wasm_exec.js defines the Go class globally
    await loadScript(wasmExecUrl);
    const go = new Go();
    const result = await WebAssembly.instantiateStreaming(fetch(wasmUrl), go.importObject);
    go.run(result.instance);
    ready = true;
  })();
  return initPromise;
}

export interface TranspileResult {
  lua: string;
  errors: string[];
  diagnostics: WasmDiagnostic[];
}

export function transpile(code: string, options: TsluaOptions): TranspileResult {
  if (!ready) return { lua: "", errors: ["WASM not loaded"], diagnostics: [] };

  const raw = tslua_transpile(code, JSON.stringify(options)) as
    | { lua?: unknown; errors?: unknown; diagnostics?: unknown }
    | null
    | undefined;
  if (!raw || typeof raw !== "object") {
    return { lua: "", errors: ["WASM returned invalid result"], diagnostics: [] };
  }
  const lua = typeof raw.lua === "string" ? raw.lua : "";
  const errors = isArrayLike(raw.errors)
    ? Array.from(raw.errors as ArrayLike<unknown>, (v) => String(v))
    : [];
  const diagnostics = isArrayLike(raw.diagnostics)
    ? Array.from(raw.diagnostics as ArrayLike<WasmDiagnostic>)
    : [];
  return { lua, errors, diagnostics };
}

function isArrayLike(v: unknown): v is ArrayLike<unknown> {
  return (
    v != null && typeof v === "object" && typeof (v as { length?: unknown }).length === "number"
  );
}

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = src;
    script.addEventListener("load", () => resolve());
    script.addEventListener("error", reject);
    document.head.appendChild(script);
  });
}
