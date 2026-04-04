import { useState, useEffect, useRef, useCallback } from "react";
import { loader } from "@monaco-editor/react";
import { Editor } from "./Editor";
import { OutputPanel } from "./OutputPanel";
import { loadWasm, transpile } from "./wasm";
import { execJs, type ExecResult } from "./exec-js";
import { execLua, type DualExecResult } from "./exec-lua";
import { compileTs } from "./compile-ts";
import { luaLanguage } from "./lua-language";
import { useHashState } from "./url-state";
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

export function App() {
  const theme = useStarlightTheme();
  const [pgState, setPgState, hashReady] = useHashState({ code: DEFAULT_CODE, target: "JIT" });
  const tsCode = pgState.code;
  const target = pgState.target;
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

  const doTranspile = useCallback(
    (code: string, tgt: string) => {
      if (loading) return;
      const t0 = performance.now();
      const result = transpile(code, tgt);
      setTranspileMs(performance.now() - t0);
      setLuaCode(result.lua);
      setErrors(result.errors);
      if (execDebounceRef.current) clearTimeout(execDebounceRef.current);
      execDebounceRef.current = setTimeout(() => runExecution(code, result.lua, tgt), 500);
    },
    [loading, runExecution],
  );

  useEffect(() => {
    if (!loading && hashReady) doTranspile(tsCode, target);
  }, [loading, hashReady]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleTsChange = useCallback(
    (value: string) => {
      setPgState((prev) => ({ ...prev, code: value }));
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => doTranspile(value, target), 300);
    },
    [target, doTranspile, setPgState],
  );

  const handleTargetChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const tgt = e.target.value;
      setPgState((prev) => ({ ...prev, target: tgt }));
      doTranspile(tsCode, tgt);
    },
    [tsCode, doTranspile, setPgState],
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
        <select value={target} onChange={handleTargetChange} className="pg-select pg-select-sm">
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

      {/* Desktop grid */}
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
            <select value={target} onChange={handleTargetChange} className="pg-select pg-select-sm">
              {LUA_TARGETS.map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
            </select>
            {transpileMs !== null && <span className="pg-timing">{transpileMs.toFixed(1)}ms</span>}
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
