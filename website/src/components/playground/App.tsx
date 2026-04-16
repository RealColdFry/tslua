import { useState, useEffect, useRef, useCallback } from "react";
import { loader } from "@monaco-editor/react";
import { Editor } from "./Editor";
import { ErrorBoundary } from "./ErrorBoundary";
import { OutputPanel } from "./OutputPanel";
import { loadWasm, transpile, type WasmDiagnostic } from "./wasm";
import { execJs, type ExecResult } from "./exec-js";
import { execLua, type DualExecResult } from "./exec-lua";
import { compileTs } from "./compile-ts";
import { luaLanguage } from "./lua-language";
import { useHashState, type PlaygroundState, type PlaygroundTsconfig } from "./url-state";
import { formatTs } from "./format-ts";
import "./playground.css";

function useStarlightTheme(): "dark" | "light" {
  const [theme, setTheme] = useState<"dark" | "light">(() => {
    if (typeof document === "undefined") return "dark";
    return document.documentElement.dataset.theme === "light" ? "light" : "dark";
  });
  useEffect(() => {
    const observer = new MutationObserver(() => {
      setTheme(document.documentElement.dataset.theme === "light" ? "light" : "dark");
    });
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);
  return theme;
}

const LUA_TARGETS = [
  { value: "JIT", label: "LuaJIT" },
  { value: "5.0", label: "Lua 5.0" },
  { value: "5.1", label: "Lua 5.1" },
  { value: "5.2", label: "Lua 5.2" },
  { value: "5.3", label: "Lua 5.3" },
  { value: "5.4", label: "Lua 5.4" },
  { value: "5.5", label: "Lua 5.5" },
  { value: "universal", label: "Universal" },
] as const;

const EMIT_MODES = [
  { value: "", label: "TSTL (default)" },
  { value: "optimized", label: "Optimized" },
] as const;

const TS_TARGETS = [
  { value: "", label: "ESNext (default)" },
  // { value: "ES5", label: "ES5" },
  { value: "ES2015", label: "ES2015" },
  { value: "ES2016", label: "ES2016" },
  { value: "ES2017", label: "ES2017" },
  { value: "ES2018", label: "ES2018" },
  { value: "ES2019", label: "ES2019" },
  { value: "ES2020", label: "ES2020" },
  { value: "ES2021", label: "ES2021" },
  { value: "ES2022", label: "ES2022" },
  { value: "ES2023", label: "ES2023" },
  { value: "ES2024", label: "ES2024" },
] as const;

const CLASS_STYLES = [
  { value: "", label: "TSTL (default)" },
  { value: "inline", label: "Inline" },
  { value: "luabind", label: "Luabind" },
  { value: "middleclass", label: "Middleclass" },
] as const;

const LUALIB_IMPORTS = [
  { value: "", label: "inline" },
  { value: "require-minimal", label: "require-minimal" },
] as const;

const DEFAULT_CODE = `// Try some TypeScript!
const greet = (name: string): string => {
  return \`Hello, \${name}!\`;
};

for (const x of [1, 2, 3]) {
  console.log(greet("world"), x);
}
`;

import { CONSOLE_DTS, langExtDts, getLuaTypesDts, AVAILABLE_TYPES } from "./builtin-types";

const EMPTY_EXEC: ExecResult = { output: [], error: null };

if (typeof window !== "undefined") {
  loader.init().then((monaco) => {
    monaco.languages.register({ id: "lua" });
    monaco.languages.setMonarchTokensProvider("lua", luaLanguage);
  });
}

type MobileTab = "ts" | "lua" | "ts-eval" | "lua-eval" | "config";

function getTarget(tsconfig: PlaygroundTsconfig): string {
  return tsconfig.tstl?.luaTarget || "JIT";
}

function ConfigSelect({
  label,
  value,
  options,
  onChange,
  note,
}: {
  label: string;
  value: string;
  options: readonly { value: string; label: string }[];
  onChange: (value: string) => void;
  note?: React.ReactNode;
}) {
  return (
    <label className="pg-config-field">
      <span className="pg-config-label">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="pg-select pg-config-select"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
      {note && <span className="pg-config-note">{note}</span>}
    </label>
  );
}

function ConfigToggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="pg-config-field pg-config-toggle">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span className="pg-config-label">{label}</span>
    </label>
  );
}

export function App() {
  return (
    <ErrorBoundary>
      <PlaygroundApp />
    </ErrorBoundary>
  );
}

function PlaygroundApp() {
  const theme = useStarlightTheme();
  const [pgState, setPgState, hashReady] = useHashState({
    code: DEFAULT_CODE,
    tsconfig: { types: ["console", "language-extensions", "lua-types"] },
  });
  const tsCode = pgState.code;
  const tsconfig = pgState.tsconfig;
  const luaTarget = getTarget(tsconfig);
  const [luaCode, setLuaCode] = useState("");
  const [errors, setErrors] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [transpileMs, setTranspileMs] = useState<number | null>(null);
  const [mobileTab, setMobileTab] = useState<MobileTab>("ts");
  const [jsResult, setJsResult] = useState<ExecResult>(EMPTY_EXEC);
  const [luaDualResult, setLuaDualResult] = useState<DualExecResult>({
    raw: EMPTY_EXEC,
    pretty: EMPTY_EXEC,
  });
  const [luaPretty, setLuaPretty] = useState(true);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [staleLua, setStaleLua] = useState(false);
  const [staleJs, setStaleJs] = useState(false);
  const [staleLuaEval, setStaleLuaEval] = useState(false);
  const [jsEvalMs, setJsEvalMs] = useState<number | null>(null);
  const [luaEvalMs, setLuaEvalMs] = useState<number | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null);
  const execDebounceRef = useRef<ReturnType<typeof setTimeout>>(null);
  // Live ref to the current tsconfig so async callbacks (debounced
  // handleTsChange) read the latest value instead of a captured snapshot.
  const tsconfigRef = useRef(tsconfig);
  useEffect(() => {
    tsconfigRef.current = tsconfig;
  }, [tsconfig]);
  // Monotonic epoch: bumped on every doTranspile entry. Async continuations
  // capture the epoch at the start of their path and check it before any
  // setState so stale results from slower prior runs can't overwrite newer ones.
  const epochRef = useRef(0);
  // True while the component is mounted. Async callbacks check this before
  // touching state so resolutions that arrive after unmount are no-ops.
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (execDebounceRef.current) clearTimeout(execDebounceRef.current);
    };
  }, []);
  const [colPct, setColPct] = useState(50);
  const [rowPct, setRowPct] = useState(60);
  const gridRef = useRef<HTMLDivElement>(null);

  const startDrag = useCallback((axis: "col" | "row", e: React.MouseEvent) => {
    e.preventDefault();
    const grid = gridRef.current;
    if (!grid) return;
    const onMove = (ev: MouseEvent) => {
      const rect = grid.getBoundingClientRect();
      if (axis === "col") {
        const pct = ((ev.clientX - rect.left) / rect.width) * 100;
        setColPct(Math.max(20, Math.min(80, pct)));
      } else {
        const pct = ((ev.clientY - rect.top) / rect.height) * 100;
        setRowPct(Math.max(20, Math.min(80, pct)));
      }
    };
    const onUp = () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
    document.body.style.cursor = axis === "col" ? "col-resize" : "row-resize";
    document.body.style.userSelect = "none";
  }, []);

  const onColDrag = useCallback((e: React.MouseEvent) => startDrag("col", e), [startDrag]);
  const onRowDrag = useCallback((e: React.MouseEvent) => startDrag("row", e), [startDrag]);

  useEffect(() => {
    loadWasm()
      .then(() => {
        if (mountedRef.current) setLoading(false);
      })
      .catch((err: Error) => {
        if (!mountedRef.current) return;
        setErrors([`Failed to load WASM: ${err.message}`]);
        setLoading(false);
      });
  }, []);

  const runExecution = useCallback(
    async (epoch: number, tsSource: string, lua: string, tgt: string, lualibBundle: string) => {
      const isCurrent = () => epoch === epochRef.current;
      try {
        const js = await compileTs(tsSource);
        if (!isCurrent()) return;
        const t0 = performance.now();
        const result = await execJs(js);
        if (!isCurrent()) return;
        setJsEvalMs(performance.now() - t0);
        setJsResult(result);
      } catch (e) {
        if (!isCurrent()) return;
        setJsEvalMs(null);
        setJsResult({ output: [], error: String(e) });
      }
      if (!isCurrent()) return;
      setStaleJs(false);
      if (lua.trim()) {
        try {
          const t0 = performance.now();
          const result = await execLua(lua, tgt, lualibBundle);
          if (!isCurrent()) return;
          setLuaEvalMs(performance.now() - t0);
          setLuaDualResult(result);
        } catch (e) {
          if (!isCurrent()) return;
          setLuaEvalMs(null);
          const errResult = { output: [], error: String(e) };
          setLuaDualResult({ raw: errResult, pretty: errResult });
        }
      } else {
        setLuaEvalMs(null);
        setLuaDualResult({ raw: EMPTY_EXEC, pretty: EMPTY_EXEC });
      }
      if (!isCurrent()) return;
      setStaleLuaEval(false);
    },
    [],
  );

  const monacoRef = useRef<Awaited<ReturnType<typeof loader.init>> | null>(null);

  const extraLibsRef = useRef<{ dispose(): void }[]>([]);
  // Cached key of the last-applied Monaco config. Skip re-apply when the
  // relevant inputs (TS target, enabled types, lua target) haven't changed;
  // otherwise Monaco flashes squigglies every keystroke during the async
  // gap between disposing old libs and adding new ones.
  const monacoKeyRef = useRef<string>("");

  const syncMonacoOptions = useCallback(async (cfg: PlaygroundTsconfig) => {
    const monaco = monacoRef.current;
    if (!monaco) return;
    const target = (cfg.compilerOptions?.target as string) || "ESNext";
    const types = (cfg.types ?? []).toSorted();
    const tgt = cfg.tstl?.luaTarget || "JIT";
    const key = JSON.stringify({ target, types, luaTarget: tgt });
    if (key === monacoKeyRef.current) return;

    const targetMap: Record<string, number> = {
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
      ES2025: 12,
      ESNext: 99,
    };

    // Pre-load all lib content before touching Monaco state so the swap is
    // atomic: Monaco's TS checker never sees a window with libs missing.
    const libs: { content: string; name: string }[] = [];
    if (types.includes("console")) libs.push({ content: CONSOLE_DTS, name: "console" });
    if (types.includes("language-extensions"))
      libs.push({ content: langExtDts, name: "language-extensions" });
    if (types.includes("lua-types")) {
      const dts = await getLuaTypesDts(tgt);
      libs.push({ content: dts, name: "lua-types" });
    }

    // Bail if another sync superseded us during the await.
    if (key === monacoKeyRef.current) return;
    monacoKeyRef.current = key;

    monaco.languages.typescript.typescriptDefaults.setCompilerOptions({
      ...monaco.languages.typescript.typescriptDefaults.getCompilerOptions(),
      target: targetMap[target] ?? 99,
      lib: [target.toLowerCase()],
      strict: true,
    });
    for (const d of extraLibsRef.current) d.dispose();
    extraLibsRef.current = libs.map(({ content, name }) =>
      monaco.languages.typescript.typescriptDefaults.addExtraLib(content, `file:///${name}.d.ts`),
    );
  }, []);

  useEffect(() => {
    // Only set monacoRef here. The initial doTranspile (fired from the
    // [loading, hashReady] effect below) handles syncMonacoOptions with the
    // restored-from-hash tsconfig, so we don't risk applying a stale snapshot.
    void loader.init().then((m) => {
      if (mountedRef.current) monacoRef.current = m;
    });
  }, []);

  const setTsluaMarkers = useCallback((diagnostics: WasmDiagnostic[]) => {
    const monaco = monacoRef.current;
    if (!monaco) return;
    const model = monaco.editor.getModel(monaco.Uri.parse("file:///main.ts"));
    if (!model) return;
    const markers = diagnostics.map((d) => ({
      startLineNumber: d.startLine,
      startColumn: d.startCol,
      endLineNumber: d.endLine,
      endColumn: d.endCol,
      message: d.message,
      severity: d.severity,
      source: "tslua",
      code: d.code ? String(d.code) : undefined,
    }));
    monaco.editor.setModelMarkers(model, "tslua", markers);
  }, []);

  const doTranspile = useCallback(
    async (code: string, cfg: PlaygroundTsconfig) => {
      if (loading) return;
      const epoch = ++epochRef.current;
      const isCurrent = () => epoch === epochRef.current;
      syncMonacoOptions(cfg);
      const tgt = cfg.tstl?.luaTarget || "JIT";
      // Build extra .d.ts files from enabled types
      const types = cfg.types ?? [];
      const extraFiles: Record<string, string> = {};
      if (types.includes("console")) extraFiles["console.d.ts"] = CONSOLE_DTS;
      if (types.includes("language-extensions"))
        extraFiles["language-extensions.d.ts"] = langExtDts;
      if (types.includes("lua-types")) extraFiles["lua-types.d.ts"] = await getLuaTypesDts(tgt);
      if (!isCurrent()) return;
      const t0 = performance.now();
      const lib = [(cfg.compilerOptions?.target as string) || "ESNext"];
      const result = transpile(code, {
        compilerOptions: { ...cfg.compilerOptions, lib },
        extraFiles,
        ...(cfg.tstl ? { tstl: cfg.tstl } : {}),
      });
      if (!isCurrent()) return;
      setTranspileMs(performance.now() - t0);
      setLuaCode(result.lua);
      setErrors(result.errors);
      setTsluaMarkers(result.diagnostics);
      setStaleLua(false);
      setStaleJs(true);
      setStaleLuaEval(true);
      if (execDebounceRef.current) clearTimeout(execDebounceRef.current);
      execDebounceRef.current = setTimeout(() => {
        if (!isCurrent()) return;
        runExecution(epoch, code, result.lua, tgt, result.lualibBundle);
      }, 500);
    },
    [loading, runExecution, setTsluaMarkers, syncMonacoOptions],
  );

  useEffect(() => {
    if (!loading && hashReady) doTranspile(tsCode, tsconfig);
  }, [loading, hashReady]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleTsChange = useCallback(
    (value: string) => {
      setPgState((prev) => ({ ...prev, code: value }));
      setStaleLua(true);
      setStaleJs(true);
      setStaleLuaEval(true);
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => doTranspile(value, tsconfigRef.current), 300);
    },
    [doTranspile, setPgState],
  );

  const [formatting, setFormatting] = useState(false);
  const [formatError, setFormatError] = useState<string | null>(null);
  const handleFormat = useCallback(() => {
    setFormatting(true);
    setFormatError(null);
    formatTs(tsCode).then(
      (formatted) => {
        handleTsChange(formatted);
        setFormatting(false);
      },
      (err) => {
        setFormatting(false);
        setFormatError(String(err));
      },
    );
  }, [tsCode, handleTsChange]);

  const updateTstl = useCallback(
    (key: string, value: string | boolean) => {
      const tstl = { ...pgState.tsconfig.tstl, [key]: value };
      if (value === "" || value === false) delete (tstl as Record<string, unknown>)[key];
      const next: PlaygroundState = {
        ...pgState,
        tsconfig: { ...pgState.tsconfig, tstl },
      };
      setPgState(next);
      doTranspile(next.code, next.tsconfig);
    },
    [pgState, setPgState, doTranspile],
  );

  const updateCompilerOption = useCallback(
    (key: string, value: string) => {
      const co = { ...pgState.tsconfig.compilerOptions, [key]: value };
      if (value === "") delete (co as Record<string, unknown>)[key];
      const next: PlaygroundState = {
        ...pgState,
        tsconfig: { ...pgState.tsconfig, compilerOptions: co },
      };
      setPgState(next);
      doTranspile(next.code, next.tsconfig);
    },
    [pgState, setPgState, doTranspile],
  );

  const toggleType = useCallback(
    (name: string) => {
      const types = pgState.tsconfig.types ?? [];
      const next: PlaygroundState = {
        ...pgState,
        tsconfig: {
          ...pgState.tsconfig,
          types: types.includes(name) ? types.filter((t) => t !== name) : [...types, name],
        },
      };
      setPgState(next);
      doTranspile(next.code, next.tsconfig);
    },
    [pgState, setPgState, doTranspile],
  );

  const sidebar = (
    <div className="pg-sidebar">
      <div className="pg-sidebar-header">
        <span>Config</span>
        <button
          className="pg-sidebar-collapse"
          onClick={() => setSidebarOpen(false)}
          title="Collapse sidebar"
        >
          &#x2039;
        </button>
      </div>
      <div className="pg-sidebar-section">
        <div className="pg-sidebar-section-title">TypeScript</div>
        <ConfigSelect
          label="Target"
          value={(tsconfig.compilerOptions?.target as string) || ""}
          options={TS_TARGETS}
          onChange={(v) => updateCompilerOption("target", v)}
        />
        <div className="pg-config-field">
          <span className="pg-config-label">Lib</span>
          <span className="pg-config-value">{`["${(tsconfig.compilerOptions?.target as string) || "ESNext"}"]`}</span>
        </div>
        <div className="pg-config-field">
          <span className="pg-config-label">Types</span>
          <div className="pg-types-list">
            {AVAILABLE_TYPES.map((t) => (
              <label key={t.name} className="pg-type-item">
                <input
                  type="checkbox"
                  checked={(tsconfig.types ?? []).includes(t.name)}
                  onChange={() => toggleType(t.name)}
                />
                <span>{t.name}</span>
                {t.url && (
                  <a
                    href={t.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="pg-type-link"
                    title={t.name}
                    onClick={(e) => e.stopPropagation()}
                  >
                    &#x2197;
                  </a>
                )}
              </label>
            ))}
          </div>
        </div>
      </div>
      <div className="pg-sidebar-section">
        <div className="pg-sidebar-section-title">tslua</div>
        <ConfigSelect
          label="Lua Target"
          value={luaTarget}
          options={LUA_TARGETS}
          onChange={(v) => updateTstl("luaTarget", v === "JIT" ? "" : v)}
          note={luaTarget === "JIT" ? "Evaluated via Lua 5.1.5 WASM." : undefined}
        />
        <ConfigSelect
          label="Emit Mode"
          value={tsconfig.tstl?.emitMode || ""}
          options={EMIT_MODES}
          onChange={(v) => updateTstl("emitMode", v)}
        />
        <ConfigSelect
          label="Class Style"
          value={tsconfig.tstl?.classStyle || ""}
          options={CLASS_STYLES}
          onChange={(v) => updateTstl("classStyle", v)}
        />
        <ConfigSelect
          label="Lualib Import"
          value={tsconfig.tstl?.luaLibImport || ""}
          options={LUALIB_IMPORTS}
          onChange={(v) => updateTstl("luaLibImport", v)}
          note={
            tsconfig.tstl?.luaLibImport === "require-minimal"
              ? "Bundle hidden from view; loaded for eval."
              : undefined
          }
        />
        <ConfigToggle
          label="noImplicitSelf"
          checked={!!tsconfig.tstl?.noImplicitSelf}
          onChange={(v) => updateTstl("noImplicitSelf", v)}
        />
        <ConfigToggle
          label="noImplicitGlobalVariables"
          checked={!!tsconfig.tstl?.noImplicitGlobalVariables}
          onChange={(v) => updateTstl("noImplicitGlobalVariables", v)}
        />
        <ConfigToggle
          label="trace"
          checked={!!tsconfig.tstl?.trace}
          onChange={(v) => updateTstl("trace", v)}
        />
      </div>
    </div>
  );

  return (
    <div className="pg-root">
      {loading && <div className="pg-loading">Loading tslua WASM...</div>}

      {/* Mobile tabs */}
      <div className="pg-mobile-tabs">
        {(["ts", "lua", "ts-eval", "lua-eval", "config"] as MobileTab[]).map((tab) => (
          <button
            key={tab}
            className={`pg-tab ${mobileTab === tab ? "active" : ""}${tab === "config" ? " pg-tab-gear" : ""}`}
            onClick={() => setMobileTab(tab)}
          >
            {
              {
                ts: "TS",
                lua: "Lua",
                "ts-eval": "JS Out",
                "lua-eval": "Lua Out",
                config: "\u2699\uFE0E",
              }[tab]
            }
          </button>
        ))}
      </div>

      {/* Mobile layout */}
      <div className="pg-mobile-layout">
        {mobileTab === "ts" && (
          <div className="pg-mobile-pane">
            <Editor
              value={tsCode}
              language="typescript"
              path="file:///main.ts"
              onChange={handleTsChange}
              theme={theme}
            />
          </div>
        )}
        {mobileTab === "lua" && (
          <div className="pg-mobile-pane">
            <Editor value={luaCode} language="lua" readOnly theme={theme} />
          </div>
        )}
        {mobileTab === "ts-eval" && (
          <OutputPanel
            label="JS Output"
            output={jsResult.output}
            error={jsResult.error}
            timeMs={jsEvalMs}
            stale={staleJs}
          />
        )}
        {mobileTab === "lua-eval" && (
          <OutputPanel
            label="Lua Output"
            output={luaPretty ? luaDualResult.pretty.output : luaDualResult.raw.output}
            error={luaPretty ? luaDualResult.pretty.error : luaDualResult.raw.error}
            timeMs={luaEvalMs}
            toggle={luaPretty}
            onToggle={() => setLuaPretty((v) => !v)}
          />
        )}
        {mobileTab === "config" && (
          <div className="pg-mobile-pane pg-mobile-config">
            {sidebar}
            <div className="pg-mobile-config-actions">
              <button className="pg-fmt-btn" onClick={handleFormat} disabled={formatting}>
                {formatting ? "Formatting..." : "Format Code"}
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Desktop layout: sidebar + grid */}
      <div className="pg-desktop">
        {sidebarOpen && sidebar}
        <div
          ref={gridRef}
          className="pg-grid"
          style={{
            gridTemplateColumns: `${colPct}% 6px 1fr`,
            gridTemplateRows: `${rowPct}% 6px 1fr`,
          }}
        >
          {/* Top-left: TypeScript editor */}
          <div className="pg-cell">
            <div className="pg-panel-header">
              {!sidebarOpen && (
                <button
                  className="pg-sidebar-toggle"
                  onClick={() => setSidebarOpen(true)}
                  title="Show config"
                >
                  &#x2699;
                </button>
              )}
              <span>TypeScript</span>
              <button
                className="pg-fmt-btn"
                onClick={handleFormat}
                disabled={formatting}
                title="Format with Prettier"
              >
                {formatting ? "..." : "Format"}
              </button>
            </div>
            <div className="pg-cell-content">
              <Editor
                value={tsCode}
                language="typescript"
                path="file:///main.ts"
                onChange={handleTsChange}
                theme={theme}
              />
            </div>
          </div>

          <div className="pg-divider pg-divider-col" onMouseDown={onColDrag} />

          {/* Top-right: Lua output */}
          <div className="pg-cell">
            <div className="pg-panel-header">
              <span>Lua</span>
              {staleLua && <span className="pg-stale" />}
              {transpileMs !== null && (
                <span className="pg-timing">{transpileMs.toFixed(1)}ms</span>
              )}
            </div>
            <div className="pg-cell-content">
              <Editor value={luaCode} language="lua" readOnly theme={theme} />
            </div>
          </div>

          <div className="pg-divider pg-divider-row" onMouseDown={onRowDrag} />
          <div className="pg-divider-center" />
          <div className="pg-divider pg-divider-row" onMouseDown={onRowDrag} />

          {/* Bottom-left: JS eval */}
          <div className="pg-cell-overflow">
            <OutputPanel
              label="JS Output"
              output={jsResult.output}
              error={jsResult.error}
              timeMs={jsEvalMs}
              stale={staleJs}
            />
          </div>

          <div className="pg-divider pg-divider-col" onMouseDown={onColDrag} />

          {/* Bottom-right: Lua eval */}
          <div className="pg-cell-overflow">
            <OutputPanel
              label="Lua Output"
              output={luaPretty ? luaDualResult.pretty.output : luaDualResult.raw.output}
              error={luaPretty ? luaDualResult.pretty.error : luaDualResult.raw.error}
              timeMs={luaEvalMs}
              toggle={luaPretty}
              onToggle={() => setLuaPretty((v) => !v)}
              stale={staleLuaEval}
            />
          </div>
        </div>
      </div>

      {(errors.length > 0 || formatError) && (
        <div className="pg-errors">
          {errors.map((e, i) => (
            <div key={i}>{e}</div>
          ))}
          {formatError && <div>Format failed: {formatError}</div>}
        </div>
      )}
    </div>
  );
}
