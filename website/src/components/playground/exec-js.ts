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

export function execJs(code: string): Promise<ExecResult> {
  return new Promise((resolve) => {
    const timeout = setTimeout(() => {
      cleanup();
      resolve({ output: [], error: "Execution timed out (3s)" });
    }, TIMEOUT_MS);

    const handler = (e: MessageEvent) => {
      if (e.source !== getIframe().contentWindow) return;
      clearTimeout(timeout);
      window.removeEventListener("message", handler);
      resolve(e.data as ExecResult);
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
const __output = [];
const __origConsole = { log: console.log, warn: console.warn, error: console.error };
console.log = (...args) => __output.push(args.map(String).join(" "));
console.warn = (...args) => __output.push("[warn] " + args.map(String).join(" "));
console.error = (...args) => __output.push("[error] " + args.map(String).join(" "));
try {
  ${escapeScript(code)}
  parent.postMessage({ output: __output, error: null }, "*");
} catch (e) {
  parent.postMessage({ output: __output, error: String(e) }, "*");
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
