// Uses Monaco's built-in TypeScript compiler to emit JS from TS source.
// Shares the editor's model (file:///main.ts) when mounted, and creates a
// stand-in at the same URI when not (e.g. on mobile with a non-TS tab active).
// Using the same URI as the editor avoids creating a second TS model, which
// would otherwise collide with it in TypeScript's global scope and produce
// spurious "Cannot redeclare block-scoped variable" diagnostics.

import { loader } from "@monaco-editor/react";

const MODEL_URI = "file:///main.ts";

export async function compileTs(source: string): Promise<string> {
  const monaco = await loader.init();
  const uri = monaco.Uri.parse(MODEL_URI);

  let model = monaco.editor.getModel(uri);
  if (!model) {
    model = monaco.editor.createModel(source, "typescript", uri);
  } else if (!model.isAttachedToEditor() && model.getValue() !== source) {
    // Editor unmounted (e.g. mobile non-TS tab): compileTs owns the model and
    // must push updates into it. When an editor is attached, the editor owns
    // the model and we leave it alone — calling setValue would reset the
    // user's cursor and selection.
    model.setValue(source);
  }

  const worker = await monaco.languages.typescript.getTypeScriptWorker();
  const client = await worker(uri);

  const output = await client.getEmitOutput(uri.toString());
  const jsFile = (output.outputFiles as { name: string; text: string }[]).find((f) =>
    f.name.endsWith(".js"),
  );

  if (!jsFile) {
    throw new Error("TypeScript compilation produced no output");
  }

  return jsFile.text;
}
