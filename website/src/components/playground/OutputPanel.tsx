interface OutputPanelProps {
  output: string[];
  error: string | null;
  label: string;
  timeMs?: number | null;
  toggle?: boolean;
  onToggle?: () => void;
  stale?: boolean;
}

export function OutputPanel({
  output,
  error,
  label,
  timeMs,
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
        {timeMs != null && <span className="pg-timing">{timeMs.toFixed(1)}ms</span>}
        {onToggle && (
          <span className="pg-segmented" onClick={onToggle}>
            <span className={`pg-seg-btn ${toggle ? "" : "active"}`}>Raw</span>
            <span className={`pg-seg-btn ${toggle ? "active" : ""}`}>Pretty</span>
          </span>
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
    </div>
  );
}
