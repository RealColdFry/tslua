export type Mode = "expression" | "function" | "module";

export interface CodegenAssertion {
  contains?: string[];
  notContains?: string[];
  matches?: string[]; // regex patterns from .toMatch(regex)
  notMatches?: string[]; // negated regex patterns from .not.toMatch(regex)
  snapshot?: string; // exact expected codegen from TSTL snapshot
}

export interface TestCase {
  name: string;
  mode: Mode;
  tsCode: string;
  tsHeader?: string;
  luaHeader?: string;
  assertion:
    | "matchJsResult"
    | "equal"
    | "diagnostic"
    | "codegen"
    | "snapshot"
    | "snapshot-resolved"
    | "other";
  otherReason?: string;
  expectedValue?: unknown;
  expectedDiagCodes?: number[];
  codegen?: CodegenAssertion;
  extraFiles?: Record<string, string>;
  returnExport?: string[];
  options?: Record<string, unknown>;
  refLua?: string;
  tstlBug?: string;
  languageExtensions?: boolean;
  luaFactory?: string;
  entryPoint?: string;
  mainFileName?: string;
  allowErrors?: boolean;
  allowDiagnostics?: boolean;
}

export interface ExtractionError {
  name: string;
  error: string;
}

export interface MigrateResult {
  specPath: string;
  outputFile: string;
  summary: string;
  skippedCases: { name: string; reason: string }[];
  bakeErrors: string[];
  extractionErrors: { name: string; error: string }[];
}

export interface CacheMiss {
  key: string;
  code: string;
  luaTarget: string;
  lib?: string[];
  types?: string[];
  languageExtensions?: boolean;
}

export interface SandboxRef {
  current: any;
}
