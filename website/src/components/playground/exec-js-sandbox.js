// This script runs inside a sandboxed iframe. It is inlined into the iframe's
// srcdoc at build time via ?raw import. Placeholders are replaced at runtime:
//   __NONCE_PLACEHOLDER__  - unique nonce for postMessage identification
//   __CODE_PLACEHOLDER__   - user code to eval

const __nonce = "__NONCE_PLACEHOLDER__";
const __output = [];
const __origConsole = { log: console.log, warn: console.warn, error: console.error };
console.log = (...args) => __output.push(args.map(String).join(" "));
console.warn = (...args) => __output.push("[warn] " + args.map(String).join(" "));
console.error = (...args) => __output.push("[error] " + args.map(String).join(" "));
// eslint-disable-next-line no-unused-vars
const print = (...args) => console.log(...args);
let __asyncError = null;
window.addEventListener("unhandledrejection", (e) => {
  e.preventDefault();
  __asyncError = e.reason;
});
function __send() {
  parent.postMessage(
    { __nonce, output: __output, error: __asyncError ? String(__asyncError) : null },
    "*",
  );
}
try {
  (0, eval)("__CODE_PLACEHOLDER__");
  // Two nested setTimeout(0) calls: the first drains the microtask queue (await
  // continuations, Promise callbacks), the second lets the browser fire
  // unhandledrejection events before we collect results.
  setTimeout(() => setTimeout(__send, 0), 0);
} catch (e) {
  parent.postMessage({ __nonce, output: __output, error: String(e) }, "*");
}
