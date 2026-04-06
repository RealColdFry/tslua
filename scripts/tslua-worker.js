// Worker thread that maintains a long-lived tslua --server process.
// Main thread communicates synchronously via SharedArrayBuffer + Atomics.
const { workerData } = require("worker_threads");
const { spawn } = require("child_process");
const readline = require("readline");

const { signal, reqBuf, respBuf, tslGoBin } = workerData;
const signalArr = new Int32Array(signal);
const reqArr = new Uint8Array(reqBuf);
const respArr = new Uint8Array(respBuf);

// Start tslua server
const server = spawn(tslGoBin, ["--server"], {
  stdio: ["pipe", "pipe", "pipe"],
});
server.unref(); // Don't keep worker event loop alive

server.stderr.on("data", (data) => {
  process.stderr.write(data);
});

const rl = readline.createInterface({ input: server.stdout });
const lineQueue = [];
let lineResolve = null;

rl.on("line", (line) => {
  if (lineResolve) {
    const r = lineResolve;
    lineResolve = null;
    r(line);
  } else {
    lineQueue.push(line);
  }
});

function nextLine() {
  if (lineQueue.length > 0) return Promise.resolve(lineQueue.shift());
  return new Promise((resolve) => {
    lineResolve = resolve;
  });
}

// Wait for ready signal
nextLine().then((readyLine) => {
  const ready = JSON.parse(readyLine);
  if (!ready.ready) {
    process.stderr.write("tslua server did not send ready\n");
    process.exit(1);
  }

  // Signal main thread that we're ready
  Atomics.store(signalArr, 0, 1);
  Atomics.notify(signalArr, 0);

  // Process requests
  async function processRequests() {
    while (true) {
      // Wait for main thread to post a request (signal[0] = 2)
      Atomics.wait(signalArr, 0, 1); // wait while value is 1 (idle)
      const currentVal = Atomics.load(signalArr, 0);
      if (currentVal === 3) break; // shutdown signal

      // Read request length from first 4 bytes of reqBuf
      const reqLen = new DataView(reqBuf).getUint32(0, true);
      const reqStr = Buffer.from(reqArr.slice(4, 4 + reqLen)).toString("utf8");

      // Send to server
      server.stdin.write(reqStr + "\n");

      // Read response
      const respLine = await nextLine();

      // Write response to shared buffer
      const respBytes = Buffer.from(respLine, "utf8");
      new DataView(respBuf).setUint32(0, respBytes.length, true);
      respArr.set(respBytes, 4);

      // Signal main thread: done (set to 1 = idle)
      Atomics.store(signalArr, 0, 1);
      Atomics.notify(signalArr, 0);
    }
  }

  processRequests().catch((err) => {
    process.stderr.write("worker error: " + err.message + "\n");
  });
});

// Cleanup on exit
process.on("exit", () => {
  try {
    server.kill();
  } catch {}
});
