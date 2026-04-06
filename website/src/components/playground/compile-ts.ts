// Uses Monaco's built-in TypeScript compiler to emit JS from TS source.
// Requires a Monaco editor mounted with path="file:///main.ts".

import { loader } from "@monaco-editor/react";

const MODEL_URI = "file:///main.ts";

export async function compileTs(): Promise<string> {
  const monaco = await loader.init();
  const uri = monaco.Uri.parse(MODEL_URI);

  const worker = await monaco.languages.typescript.getTypeScriptWorker();
  const client = await worker(uri);

  const output = await client.getEmitOutput(uri.toString());
  const jsFile = output.outputFiles.find((f: any) => f.name.endsWith(".js"));

  if (!jsFile) {
    throw new Error("TypeScript compilation produced no output");
  }

  return jsFile.text;
}
