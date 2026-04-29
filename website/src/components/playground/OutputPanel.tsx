import type { MemoryStats } from "./exec-lua";

interface OutputPanelProps {
  output: string[];
  error: string | null;
  label: string;
  timeMs?: number | null;
  memory?: MemoryStats | null | undefined;
  toggle?: boolean;
  onToggle?: () => void;
  stale?: boolean;
}

function formatKb(kb: number): { value: string; unit: string } {
  if (kb < 1) return { value: (kb * 1024).toFixed(0), unit: "B" };
  if (kb < 1024) return { value: kb.toFixed(1), unit: "KB" };
  return { value: (kb / 1024).toFixed(2), unit: "MB" };
}

function Stat({
  value,
  unit,
  full,
  short,
  title,
}: {
  value: string;
  unit: string;
  full: string;
  short: string;
  title: string;
}) {
  return (
    <span className="pg-stat" title={title}>
      <span className="pg-stat-value">{value}</span>
      <span className="pg-stat-unit"> {unit}</span>
      <span className="pg-stat-word"> {full}</span>
      <span className="pg-stat-word-short"> {short}</span>
    </span>
  );
}

export function OutputPanel({
  output,
  error,
  label,
  timeMs,
  memory,
  toggle,
  onToggle,
  stale,
}: OutputPanelProps) {
  const hasContent = output.length > 0 || error;

  return (
    <div className="pg-output">
      <div className="pg-panel-header">
        {label}
        {stale && <span className="pg-stale" />}
        {timeMs != null && (
          <span className="pg-timing">
            <span className="pg-stat-value">{timeMs.toFixed(1)}</span> ms
          </span>
        )}
        {onToggle && (
          <button className="pg-segmented" onClick={onToggle} type="button">
            <span className={`pg-seg-btn ${toggle ? "" : "active"}`}>Raw</span>
            <span className={`pg-seg-btn ${toggle ? "active" : ""}`}>Pretty</span>
          </button>
        )}
      </div>
      <div className="pg-output-body">
        {!hasContent && <span className="pg-output-empty">No output</span>}
        {output.map((line, i) => (
          <div key={i} className="pg-output-line">
            {line}
          </div>
        ))}
        {error && <div className="pg-output-error">{error}</div>}
      </div>
      {memory &&
        (() => {
          const alloc = formatKb(memory.allocatedKb);
          const retained = formatKb(memory.retainedKb);
          return (
            <div className="pg-output-stats">
              <Stat
                value={alloc.value}
                unit={alloc.unit}
                full="allocated"
                short="alloc"
                title="Lua heap growth at end of user code, before a forced GC. Includes live objects and uncollected garbage."
              />
              <span className="pg-stat-sep">·</span>
              <Stat
                value={retained.value}
                unit={retained.unit}
                full="retained"
                short="ret"
                title="Lua heap surviving a forced full GC after user code. Approximates rooted (referenced) memory."
              />
              <span className="pg-stat-sep">·</span>
              <Stat
                value={memory.gcMs.toFixed(2)}
                unit="ms"
                full="GC"
                short="GC"
                title="Wall-clock time of the forced full GC pause taken after user code."
              />
              <span className="pg-stat-sep">·</span>
              <Stat
                value={memory.wasmHeapMb.toFixed(1)}
                unit="MB"
                full="heap"
                short="heap"
                title="WASM linear memory committed to the Lua interpreter. Monotonic across the worker's lifetime."
              />
            </div>
          );
        })()}
    </div>
  );
}
