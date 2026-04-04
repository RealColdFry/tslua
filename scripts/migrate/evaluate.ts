import ts from "typescript";
import vm from "vm";
import type { Mode } from "./types.ts";

// Mirrors TSTL's ExecutionError — returned when JS evaluation throws.
// Serializes as {name = "ExecutionError", message = "..."} to match Lua error behavior.
class ExecutionError extends Error {
  override name = "ExecutionError";
}

export function evaluateJS(
  mode: Mode,
  code: string,
  extraFiles?: Record<string, string>,
  returnExport?: string[],
  tsHeader?: string,
  languageExtensions?: boolean,
  tsCompilerOptions?: Record<string, unknown>,
  allowErrors?: boolean,
): unknown {
  const header = tsHeader ? tsHeader + "\n" : "";
  let fullCode: string;
  switch (mode) {
    case "expression":
      fullCode = `${header}export const __result = ${code};`;
      break;
    case "function":
      fullCode = `${header}export function __main() {${code}}`;
      break;
    case "module":
      fullCode = header + code;
      break;
  }

  const compilerOptions: ts.CompilerOptions = {
    target: ts.ScriptTarget.ES2017,
    module: ts.ModuleKind.CommonJS,
    strict: true,
    ...(tsCompilerOptions?.experimentalDecorators ? { experimentalDecorators: true } : {}),
    ...(tsCompilerOptions?.jsx != null ? { jsx: tsCompilerOptions.jsx as ts.JsxEmit } : {}),
    ...(tsCompilerOptions?.jsxFactory
      ? { jsxFactory: tsCompilerOptions.jsxFactory as string }
      : {}),
    ...(tsCompilerOptions?.jsxFragmentFactory
      ? { jsxFragmentFactory: tsCompilerOptions.jsxFragmentFactory as string }
      : {}),
  };

  const compiledExtras: Record<string, string> = {};
  for (const [name, source] of Object.entries(extraFiles || {})) {
    const result = ts.transpileModule(source, { compilerOptions });
    const modName = "./" + name.replace(/\.ts$/, "");
    compiledExtras[modName] = result.outputText;
  }

  const fileName = compilerOptions.jsx ? "main.tsx" : "main.ts";
  const jsResult = ts.transpileModule(fullCode, { compilerOptions, fileName });

  const mainExports: any = {};
  const mainModule = { exports: mainExports };
  const context: any = vm.createContext({
    exports: mainExports,
    module: mainModule,
    require: (name: string) => {
      if (compiledExtras[name]) {
        // Run in same context (shared globals), but swap exports/module
        // to match TSTL's test harness behavior
        const modExports: any = {};
        context.exports = modExports;
        context.module = { exports: modExports };
        vm.runInContext(compiledExtras[name], context);
        const result = context.module.exports;
        context.exports = mainExports;
        context.module = mainModule;
        return result;
      }
      return {};
    },
    console,
    // Polyfill language extensions for JS evaluation (mirrors TSTL's withLanguageExtensions)
    ...(languageExtensions && { $multi: (...args: unknown[]) => args }),
  });
  if (allowErrors) {
    try {
      vm.runInContext(jsResult.outputText, context);
    } catch (e: any) {
      return new ExecutionError(e.message ?? String(e));
    }
  } else {
    vm.runInContext(jsResult.outputText, context);
  }

  let result: unknown;
  if (allowErrors) {
    try {
      result = extractResult(mode, mainModule, returnExport);
    } catch (e: any) {
      return new ExecutionError(e.message ?? String(e));
    }
  } else {
    result = extractResult(mode, mainModule, returnExport);
  }
  return result;
}

function extractResult(mode: Mode, mainModule: { exports: any }, returnExport?: string[]): unknown {
  switch (mode) {
    case "expression":
      return mainModule.exports.__result;
    case "function":
      return mainModule.exports.__main();
    case "module": {
      let result: unknown = mainModule.exports;
      if (returnExport) {
        for (const name of returnExport) {
          result = (result as any)[name];
        }
      }
      return result;
    }
  }
}
