// Lazy-loads prettier + TypeScript plugin on first use to avoid impacting
// initial page load (~300-400KB gzipped).

let prettierPromise: Promise<typeof import("prettier/standalone")> | null = null;
let pluginPromise: Promise<typeof import("prettier/plugins/typescript")> | null = null;
let estreePromise: Promise<typeof import("prettier/plugins/estree")> | null = null;

export async function formatTs(code: string): Promise<string> {
  prettierPromise ??= import("prettier/standalone");
  pluginPromise ??= import("prettier/plugins/typescript");
  estreePromise ??= import("prettier/plugins/estree");
  const [prettier, tsPlugin, estreePlugin] = await Promise.all([
    prettierPromise,
    pluginPromise,
    estreePromise,
  ]);
  return prettier.format(code, {
    parser: "typescript",
    plugins: [tsPlugin, estreePlugin],
    printWidth: 100,
  });
}
