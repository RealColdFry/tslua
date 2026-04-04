import wasmUrl from "../../assets/wasm/tslua.wasm?url";
import wasmExecUrl from "../../assets/wasm/wasm_exec.js?url";

declare class Go {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): void;
}

declare function tslua_transpile(
  code: string,
  target: string,
): { lua: string; errors: { length: number; [i: number]: string } };

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
}

export function transpile(code: string, target: string): TranspileResult {
  if (!ready) return { lua: "", errors: ["WASM not loaded"] };

  const result = tslua_transpile(code, target);
  const errors: string[] = [];
  for (let i = 0; i < result.errors.length; i++) {
    errors.push(result.errors[i]);
  }
  return { lua: result.lua || "", errors };
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
