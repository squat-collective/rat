// Builds the portal plugin bundle: esbuild bundles our UI code together with
// Recharts into a single IIFE that the portal loads at runtime.
//
// React itself is NOT bundled. Plugin components are rendered by the portal's
// own React, so they must use the *same* React instance — two copies break
// hooks ("invalid hook call"). The portal exposes that instance on
// window.React / window.ReactDOM; the plugin below rewrites every `react`
// import to a shim that re-exports those globals.

import esbuild from "esbuild";
import path from "node:path";
import { fileURLToPath } from "node:url";

const dir = path.dirname(fileURLToPath(import.meta.url));

// Rewrite React imports (ours and Recharts') to the window-global shims.
const reactGlobals = {
  name: "react-globals",
  setup(build) {
    const shims = {
      react: "shims/react.js",
      "react-dom": "shims/react-dom.js",
      "react-dom/client": "shims/react-dom.js",
      "react/jsx-runtime": "shims/jsx-runtime.js",
      "react/jsx-dev-runtime": "shims/jsx-runtime.js",
    };
    build.onResolve({ filter: /^react(-dom)?(\/.*)?$/ }, (args) => {
      const shim = shims[args.path];
      return shim ? { path: path.join(dir, shim) } : null;
    });
  },
};

await esbuild.build({
  entryPoints: [path.join(dir, "src/index.jsx")],
  bundle: true,
  minify: true,
  format: "iife",
  target: "es2019",
  jsx: "transform",
  jsxFactory: "React.createElement",
  jsxFragment: "React.Fragment",
  legalComments: "none",
  plugins: [reactGlobals],
  outfile: path.join(dir, "dist/bundle.js"),
  logLevel: "info",
});

console.log("charts plugin UI bundle written to dist/bundle.js");
