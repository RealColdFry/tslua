// Executes JavaScript in a sandboxed iframe with no network access.
// CSP blocks all fetch/XHR/WebSocket. Sandbox blocks DOM/navigation/origin access.

const TIMEOUT_MS = 3000;

export interface ExecResult {
  output: string[];
  error: string | null;
}

let iframe: HTMLIFrameElement | null = null;

function getIframe(): HTMLIFrameElement {
  if (iframe && iframe.parentNode) return iframe;
  iframe = document.createElement("iframe");
  iframe.sandbox.add("allow-scripts");
  iframe.style.display = "none";
  document.body.appendChild(iframe);
  return iframe;
}

// User-provided code runs via eval inside the iframe and can postMessage to
// parent with arbitrary shapes. We tag our wrapper's message with a per-call
// nonce so the handler can distinguish it from user-origin messages.
function coerceExecResult(data: unknown): ExecResult {
  if (typeof data !== "object" || data === null) {
    return { output: [], error: "Invalid iframe response" };
  }
  const d = data as { output?: unknown; error?: unknown };
  const output = Array.isArray(d.output) ? d.output.map((v) => String(v)) : [];
  const error = typeof d.error === "string" ? d.error : d.error == null ? null : String(d.error);
  return { output, error };
}

let nonceCounter = 0;
function makeNonce(): string {
  // Per-call unique token to distinguish our wrapper's messages from anything
  // the user's code might postMessage. Not a security primitive, so plain
  // Math.random is fine (and avoids crypto.randomUUID availability issues).
  nonceCounter++;
  return `${Date.now().toString(36)}-${nonceCounter}-${Math.random().toString(36).slice(2)}`;
}

export function execJs(code: string): Promise<ExecResult> {
  return new Promise((resolve) => {
    const nonce = makeNonce();
    let settled = false;
    const settle = (result: ExecResult) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      window.removeEventListener("message", handler);
      resolve(result);
    };
    const timeout = setTimeout(() => {
      cleanup();
      settle({ output: [], error: "Execution timed out (3s)" });
    }, TIMEOUT_MS);

    const handler = (e: MessageEvent) => {
      if (e.source !== getIframe().contentWindow) return;
      const d = e.data as { __nonce?: unknown } | null | undefined;
      if (!d || d.__nonce !== nonce) return;
      settle(coerceExecResult(e.data));
    };
    window.addEventListener("message", handler);

    const f = getIframe();
    f.srcdoc = `
<!DOCTYPE html>
<html>
<head>
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval';">
</head>
<body>
<script>
const __nonce = ${JSON.stringify(nonce)};
const __output = [];
const __origConsole = { log: console.log, warn: console.warn, error: console.error };
console.log = (...args) => __output.push(args.map(String).join(" "));
console.warn = (...args) => __output.push("[warn] " + args.map(String).join(" "));
console.error = (...args) => __output.push("[error] " + args.map(String).join(" "));
const print = (...args) => console.log(...args);
try {
  ${escapeScript(code)}
  parent.postMessage({ __nonce: __nonce, output: __output, error: null }, "*");
} catch (e) {
  parent.postMessage({ __nonce: __nonce, output: __output, error: String(e) }, "*");
}
</script>
</body>
</html>`;
  });
}

function cleanup() {
  if (iframe && iframe.parentNode) {
    iframe.parentNode.removeChild(iframe);
    iframe = null;
  }
}

function escapeScript(code: string): string {
  // Wrap in a function to avoid top-level return issues, and use indirect eval
  // to run in global scope.
  return `(0, eval)(${JSON.stringify(code)});`;
}
