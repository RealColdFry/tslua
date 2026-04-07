#!/usr/bin/env node

const process = require("node:process");
const child_process = require("node:child_process");

const PLATFORMS = {
  darwin: { x64: "@tslua/cli-darwin-x64", arm64: "@tslua/cli-darwin-arm64" },
  linux: { x64: "@tslua/cli-linux-x64", arm64: "@tslua/cli-linux-arm64" },
  win32: { x64: "@tslua/cli-win32-x64" },
};

const platformPkgs = PLATFORMS[process.platform];
if (!platformPkgs) {
  console.error(`Unsupported platform: ${process.platform}`);
  process.exit(1);
}
const pkg = platformPkgs[process.arch];
if (!pkg) {
  console.error(`Unsupported architecture: ${process.platform}-${process.arch}`);
  process.exit(1);
}

const bin = process.platform === "win32" ? "tslua.exe" : "tslua";
const exePath = require.resolve(`${pkg}/bin/${bin}`);

try {
  child_process.execFileSync(exePath, process.argv.slice(2), {
    stdio: "inherit",
  });
} catch (e) {
  if (e.status != null) {
    process.exitCode = e.status;
  } else {
    throw e;
  }
}
