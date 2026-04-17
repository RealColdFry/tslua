// Load real TSTL diagnostic factories so migrated codes track upstream changes
// automatically. Loaded dynamically via require to keep TSTL out of the script
// tsconfig. Import src entry first so the diagnostic counter follows the same
// order as the TSTL jest test environment.

/* eslint-disable @typescript-eslint/no-require-imports */
require("../../extern/tstl/src");
const tstlTransformDiags = require("../../extern/tstl/src/transformation/utils/diagnostics");
const tstlTranspileDiags = require("../../extern/tstl/src/transpilation/diagnostics");
const tstlCliDiags = require("../../extern/tstl/src/cli/diagnostics");
/* eslint-enable @typescript-eslint/no-require-imports */

type DiagModule = Record<string, { code?: number } | unknown>;

export const tstlDiagModules: DiagModule[] = [
  tstlTransformDiags as DiagModule,
  tstlTranspileDiags as DiagModule,
  tstlCliDiags as DiagModule,
];

export function tstlDiagCode(name: string): number {
  for (const mod of tstlDiagModules) {
    const factory = (mod as any)[name];
    if (factory && typeof factory.code === "number") return factory.code;
  }
  throw new Error(`Unknown TSTL diagnostic: ${name}`);
}
