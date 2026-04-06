// Jest globalSetup: starts a tslua socket server for the test run.
const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");
const readline = require("readline");

const SOCKET_PATH = path.join(__dirname, "../tmp/tslua-jest.sock");
const TSLUA_BIN = path.join(__dirname, "../tslua");

module.exports = async function () {
  // Ensure tmp dir exists
  fs.mkdirSync(path.join(__dirname, "../tmp"), { recursive: true });

  // Clean stale socket
  try {
    fs.unlinkSync(SOCKET_PATH);
  } catch {}

  const server = spawn(TSLUA_BIN, ["--server", "--socket", SOCKET_PATH], {
    stdio: ["ignore", "pipe", "inherit"],
  });

  // Wait for ready signal on stdout
  await new Promise((resolve, reject) => {
    const rl = readline.createInterface({ input: server.stdout });
    const timeout = setTimeout(() => reject(new Error("tslua server startup timeout")), 10000);
    rl.on("line", (line) => {
      const msg = JSON.parse(line);
      if (msg.ready) {
        clearTimeout(timeout);
        rl.close();
        resolve();
      }
    });
    server.on("error", reject);
    server.on("exit", (code) => reject(new Error(`tslua server exited with ${code}`)));
  });

  // Store for teardown
  globalThis.__tslua_server = server;
  globalThis.__tslua_socket = SOCKET_PATH;
};
