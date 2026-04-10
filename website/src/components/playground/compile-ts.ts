// Uses Monaco's built-in TypeScript compiler to emit JS from TS source.
// Requires a Monaco editor mounted with path="file:///main.ts".

import { loader } from "@monaco-editor/react";

const MODEL_URI = "file:///main.ts";

// Map our target strings to Monaco's ScriptTarget enum values.
const TARGET_MAP: Record<string, number> = {
  ES5: 1,
  ES2015: 2,
  ES2016: 3,
  ES2017: 4,
  ES2018: 5,
  ES2019: 6,
  ES2020: 7,
  ES2021: 8,
  ES2022: 9,
  ES2023: 10,
  ES2024: 11,
  ESNext: 99,
};

export async function compileTs(target?: string): Promise<string> {
  const monaco = await loader.init();
  const uri = monaco.Uri.parse(MODEL_URI);

  const scriptTarget = (target && TARGET_MAP[target]) ?? TARGET_MAP.ESNext;
  monaco.languages.typescript.typescriptDefaults.setCompilerOptions({
    ...monaco.languages.typescript.typescriptDefaults.getCompilerOptions(),
    target: scriptTarget,
    lib: [(target || "ESNext").toLowerCase()],
  });

  const worker = await monaco.languages.typescript.getTypeScriptWorker();
  const client = await worker(uri);

  const output = await client.getEmitOutput(uri.toString());
  const jsFile = output.outputFiles.find((f: any) => f.name.endsWith(".js"));

  if (!jsFile) {
    throw new Error("TypeScript compilation produced no output");
  }

  return jsFile.text;
}
