// @ts-check

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";

const version = requiredEnvVar("TSLUA_VERSION");

const NPM_ORG = "tslua";

const PLATFORMS = {
  "linux-amd64": { os: "linux", cpu: "x64" },
  "linux-arm64": { os: "linux", cpu: "arm64" },
  "darwin-amd64": { os: "darwin", cpu: "x64" },
  "darwin-arm64": { os: "darwin", cpu: "arm64" },
  "windows-amd64": { os: "win32", cpu: "x64" },
};

const commonPackageJson = {
  version,
  license: "MIT",
  author: "Cold Fry",
  repository: { type: "git", url: "https://github.com/RealColdFry/tslua" },
  homepage: "https://github.com/RealColdFry/tslua",
  publishConfig: { access: "public" },
};

const repoRoot = path.join(import.meta.dirname, "..");
const npmDir = path.join(repoRoot, "npm");
const artifactsDir = path.join(repoRoot, "artifacts");

// Generate platform packages
await Promise.all(
  Object.entries(PLATFORMS).map(async ([key, { os, cpu }]) => {
    const suffix = `${os}-${cpu}`.replace("win32", "win32");
    const npmName = `@${NPM_ORG}/cli-${suffix}`;
    const packageDir = path.join(npmDir, `cli-${suffix}`);
    const binaryName = os === "win32" ? "tslua.exe" : "tslua";
    const artifactName = `tslua-${key}${os === "win32" ? ".exe" : ""}`;

    await fs.rm(packageDir, { recursive: true, force: true });
    await fs.mkdir(path.join(packageDir, "bin"), { recursive: true });

    await Promise.all([
      fs.writeFile(
        path.join(packageDir, "package.json"),
        JSON.stringify(
          {
            ...commonPackageJson,
            name: npmName,
            description: `tslua platform binary (${os}-${cpu})`,
            os: [os],
            cpu: [cpu],
            files: ["bin/"],
          },
          null,
          2,
        ) + "\n",
      ),
      fs.copyFile(
        path.join(artifactsDir, artifactName),
        path.join(packageDir, "bin", binaryName),
      ).then(() =>
        os !== "win32"
          ? fs.chmod(path.join(packageDir, "bin", binaryName), 0o755)
          : undefined,
      ),
    ]);
  }),
);

// Update main package version and optionalDependencies
const mainPkgPath = path.join(npmDir, "tslua", "package.json");
const mainPkg = JSON.parse(await fs.readFile(mainPkgPath, "utf-8"));
mainPkg.version = version;
for (const dep of Object.keys(mainPkg.optionalDependencies || {})) {
  mainPkg.optionalDependencies[dep] = version;
}
await fs.writeFile(mainPkgPath, JSON.stringify(mainPkg, null, 2) + "\n");

function requiredEnvVar(/** @type {string} */ name) {
  const value = process.env[name];
  assert.ok(value != null, `missing $${name} env variable`);
  return value;
}
