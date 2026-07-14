import { cp, mkdir, rm } from "node:fs/promises";
import { build } from "esbuild";

await rm("dist", { recursive: true, force: true });
await mkdir("dist", { recursive: true });

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
