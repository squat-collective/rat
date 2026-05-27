// Resolves `import React from "react"` (and all named imports) to
// the host portal's React instance, exposed at window.React.
// CommonJS-style so esbuild's interop wraps it correctly.
"use strict";
const R = (typeof window !== "undefined" && window.React) || {};
module.exports = R;
module.exports.default = R;
