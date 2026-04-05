import { useState, useEffect, useRef, useCallback } from "react";
import { loader } from "@monaco-editor/react";
import type { editor as monacoEditor } from "monaco-editor";
import { Editor } from "./Editor";
import { OutputPanel } from "./OutputPanel";
import { loadWasm, transpile, type WasmDiagnostic } from "./wasm";
import { execJs, type ExecResult } from "./exec-js";
import { execLua, type DualExecResult } from "./exec-lua";
import { compileTs } from "./compile-ts";
import { luaLanguage } from "./lua-language";
import { useHashState, type PlaygroundState, type PlaygroundTsconfig } from "./url-state";
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

const CLASS_STYLES = [
  { value: "", label: "TSTL (default)" },
  { value: "luabind", label: "Luabind" },
  { value: "middleclass", label: "Middleclass" },
  { value: "inline", label: "Inline" },
] as const;

const DEFAULT_CODE = `// Try some TypeScript!
const greet = (name: string): string => {
  return \`Hello, \${name}!\`;
};

for (const x of [1, 2, 3]) {
  console.log(greet("world"), x);
}
`;

const EMPTY_EXEC: ExecResult = { output: [], error: null };

if (typeof window !== "undefined") {
  loader.init().then((monaco) => {
    monaco.languages.register({ id: "lua" });
    monaco.languages.setMonarchTokensProvider("lua", luaLanguage);
  });
}

type MobileTab = "ts" | "lua" | "ts-eval" | "lua-eval";

function getTarget(tsconfig: PlaygroundTsconfig): string {
  return tsconfig.tstl?.luaTarget || "JIT";
}

function ConfigSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly { value: string; label: string }[];
  onChange: (value: string) => void;
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
  const theme = useStarlightTheme();
  const [pgState, setPgState, hashReady] = useHashState({
    code: DEFAULT_CODE,
    tsconfig: {},
  });
  const tsCode = pgState.code;
  const tsconfig = pgState.tsconfig;
  const target = getTarget(tsconfig);
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
  const [jsEvalMs, setJsEvalMs] = useState<number | null>(null);
  const [luaEvalMs, setLuaEvalMs] = useState<number | null>(null);
  const tsEditorRef = useRef<monacoEditor.IStandaloneCodeEditor>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null);
  const execDebounceRef = useRef<ReturnType<typeof setTimeout>>(null);
  const [colPct, setColPct] = useState(50);
  const [rowPct, setRowPct] = useState(60);
  const gridRef = useRef<HTMLDivElement>(null);

  const useDrag = (axis: "col" | "row") =>
    useCallback((e: React.MouseEvent) => {
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

  const onColDrag = useDrag("col");
  const onRowDrag = useDrag("row");

  useEffect(() => {
    loadWasm()
      .then(() => setLoading(false))
      .catch((err) => {
        setErrors([`Failed to load WASM: ${err.message}`]);
        setLoading(false);
      });
  }, []);

  const runExecution = useCallback(async (_tsSource: string, lua: string, tgt: string) => {
    try {
      const js = await compileTs();
      const t0 = performance.now();
      const result = await execJs(js);
      setJsEvalMs(performance.now() - t0);
      setJsResult(result);
    } catch (e) {
      setJsEvalMs(null);
      setJsResult({ output: [], error: String(e) });
    }
    if (lua.trim()) {
      try {
        const t0 = performance.now();
        const result = await execLua(lua, tgt);
        setLuaEvalMs(performance.now() - t0);
        setLuaDualResult(result);
      } catch (e) {
        setLuaEvalMs(null);
        const errResult = { output: [], error: String(e) };
        setLuaDualResult({ raw: errResult, pretty: errResult });
      }
    } else {
      setLuaEvalMs(null);
      setLuaDualResult({ raw: EMPTY_EXEC, pretty: EMPTY_EXEC });
    }
  }, []);

  const monacoRef = useRef<typeof import("monaco-editor") | null>(null);

  useEffect(() => {
    loader.init().then((m) => { monacoRef.current = m; });
  }, []);

  const setTsluaMarkers = useCallback((diagnostics: WasmDiagnostic[]) => {
    const editor = tsEditorRef.current;
    const monaco = monacoRef.current;
    if (!editor || !monaco) return;
    const model = editor.getModel();
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
    (code: string, cfg: PlaygroundTsconfig) => {
      if (loading) return;
      const tgt = cfg.tstl?.luaTarget || "JIT";
      const t0 = performance.now();
      const result = transpile(code, {
        compilerOptions: cfg.compilerOptions,
        tstl: cfg.tstl,
      });
      setTranspileMs(performance.now() - t0);
      setLuaCode(result.lua);
      setErrors(result.errors);
      setTsluaMarkers(result.diagnostics);
      if (execDebounceRef.current) clearTimeout(execDebounceRef.current);
      execDebounceRef.current = setTimeout(() => runExecution(code, result.lua, tgt), 500);
    },
    [loading, runExecution, setTsluaMarkers],
  );

  useEffect(() => {
    if (!loading && hashReady) doTranspile(tsCode, tsconfig);
  }, [loading, hashReady]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleTsChange = useCallback(
    (value: string) => {
      setPgState((prev) => ({ ...prev, code: value }));
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => doTranspile(value, tsconfig), 300);
    },
    [tsconfig, doTranspile, setPgState],
  );

  const updateTstl = useCallback(
    (key: string, value: string | boolean) => {
      setPgState((prev) => {
        const tstl = { ...prev.tsconfig.tstl, [key]: value };
        // Clean up default/falsy values
        if (value === "" || value === false) delete (tstl as Record<string, unknown>)[key];
        const next: PlaygroundState = {
          ...prev,
          tsconfig: { ...prev.tsconfig, tstl },
        };
        doTranspile(prev.code, next.tsconfig);
        return next;
      });
    },
    [doTranspile, setPgState],
  );

  const sidebar = (
    <div className="pg-sidebar">
      <div className="pg-sidebar-header">Config</div>
      <div className="pg-sidebar-section">
        <div className="pg-sidebar-section-title">tslua</div>
        <ConfigSelect
          label="Lua Target"
          value={target}
          options={LUA_TARGETS}
          onChange={(v) => updateTstl("luaTarget", v === "JIT" ? "" : v)}
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
        {(["ts", "lua", "ts-eval", "lua-eval"] as MobileTab[]).map((tab) => (
          <button
            key={tab}
            className={`pg-tab ${mobileTab === tab ? "active" : ""}`}
            onClick={() => setMobileTab(tab)}
          >
            {{ ts: "TS", lua: "Lua", "ts-eval": "JS Out", "lua-eval": "Lua Out" }[tab]}
          </button>
        ))}
        <select
          value={target}
          onChange={(e) => updateTstl("luaTarget", e.target.value === "JIT" ? "" : e.target.value)}
          className="pg-select pg-select-sm"
        >
          {LUA_TARGETS.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
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
              onEditorMount={(e) => { tsEditorRef.current = e; }}
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
            toggleLabel={luaPretty ? "Pretty" : "Raw"}
          />
        )}
      </div>

      {/* Desktop layout: sidebar + grid */}
      <div className="pg-desktop">
        {sidebar}
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
            <div className="pg-panel-header">TypeScript</div>
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
              toggleLabel={luaPretty ? "Pretty" : "Raw"}
            />
          </div>
        </div>
      </div>

      {errors.length > 0 && (
        <div className="pg-errors">
          {errors.map((e, i) => (
            <div key={i}>{e}</div>
          ))}
        </div>
      )}
    </div>
  );
}
