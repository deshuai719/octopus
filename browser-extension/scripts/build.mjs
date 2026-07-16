import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { promisify } from "node:util";
import { build } from "esbuild";

const execFileAsync = promisify(execFile);
const repositoryRoot = resolve(process.cwd(), "..");
const updaterOutput = resolve(process.cwd(), "dist", "octopus-extension-updater.exe");

await rm("dist", { recursive: true, force: true });
await mkdir("dist", { recursive: true });

await execFileAsync("go", [
  "build",
  "-trimpath",
  "-ldflags=-s -w -H=windowsgui",
  "-o",
  updaterOutput,
  "./cmd/octopus-extension-updater",
], {
  cwd: repositoryRoot,
  env: { ...process.env, GOOS: "windows", GOARCH: "amd64", CGO_ENABLED: "0" },
});

const updater = await readFile(updaterOutput);
await writeFile("dist/updater-integrity.json", `${JSON.stringify({
  version: "0.1.0",
  size: updater.byteLength,
  sha256: createHash("sha256").update(updater).digest("hex"),
}, null, 2)}\n`);

await build({
  entryPoints: {
    worker: "src/worker.ts",
    sidepanel: "src/sidepanel.ts",
  },
  bundle: true,
  format: "esm",
  target: "chrome120",
  outdir: "dist",
  sourcemap: false,
  minify: false,
});

await Promise.all([
  cp("manifest.json", "dist/manifest.json"),
  cp("static/sidepanel.html", "dist/sidepanel.html"),
  cp("static/sidepanel.css", "dist/sidepanel.css"),
]);
