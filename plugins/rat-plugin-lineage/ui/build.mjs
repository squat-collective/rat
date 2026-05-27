// build.mjs — esbuild config for the plugin UI bundle.
//
// React + ReactDOM are NOT bundled — they're aliased to small shims
// that return window.React / window.ReactDOM at runtime. The host
// portal exposes those globals (see portal/src/components/plugins/
// react-globals.tsx). Reusing the host's React instance is essential:
// two React copies would break hooks across the boundary.
//
// @xyflow/react and dagre ARE bundled — they're plugin-specific
// dependencies and don't need to coexist with the host.
//
// React Flow ships a CSS file; we load it as text and inject it into
// the document head at runtime so the plugin doesn't depend on the
// portal serving the stylesheet.

import { build } from "esbuild";
import { mkdirSync } from "fs";

mkdirSync("dist", { recursive: true });

const watch = process.argv.includes("--watch");

const ctx = {
  entryPoints: ["src/index.tsx"],
  bundle: true,
  outfile: "dist/bundle.js",
  format: "iife",
  platform: "browser",
  target: "es2020",
  minify: !watch,
  sourcemap: watch ? "inline" : false,
  loader: {
    ".css": "text",
  },
  jsx: "automatic",
  jsxImportSource: "react",
  // Resolve `react` and friends to our shims, which return window.React.
  alias: {
    react: "./shims/react.cjs",
    "react-dom": "./shims/react-dom.cjs",
    "react-dom/client": "./shims/react-dom.cjs",
    "react/jsx-runtime": "./shims/jsx-runtime.cjs",
    "react/jsx-dev-runtime": "./shims/jsx-runtime.cjs",
  },
  logLevel: "info",
};

await build(ctx);
console.log("built dist/bundle.js");
