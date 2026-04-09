import wasmUrl from "../../assets/wasm/tslua.wasm?url";
import wasmExecUrl from "../../assets/wasm/wasm_exec.js?url";

declare class Go {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): void;
}

declare function tslua_transpile(
  code: string,
  optionsJson: string,
): {
  lua: string;
  errors: { length: number; [i: number]: string };
  diagnostics: { length: number; [i: number]: WasmDiagnostic } | null;
};

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

  const result = tslua_transpile(code, JSON.stringify(options));
  const errors: string[] = [];
  for (let i = 0; i < result.errors.length; i++) {
    errors.push(result.errors[i]);
  }
  const diagnostics: WasmDiagnostic[] = [];
  if (result.diagnostics) {
    for (let i = 0; i < result.diagnostics.length; i++) {
      diagnostics.push(result.diagnostics[i]);
    }
  }
  return { lua: result.lua || "", errors, diagnostics };
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
