// Flat ESLint config (ESLint 9 / Next 16).
//
// Next 16 removed `next lint`; the portal now runs ESLint directly. The
// `eslint-config-next/core-web-vitals` entrypoint exports a flat-config array
// (the v9 successor to the old `.eslintrc.json` `extends: "next/core-web-vitals"`).
import nextCoreWebVitals from "eslint-config-next/core-web-vitals";

const config = [
  {
    ignores: [".next/**", "node_modules/**", "next-env.d.ts"],
  },
  ...nextCoreWebVitals,
  {
    // The React-Compiler-aware rules shipped by eslint-plugin-react-hooks v6
    // (bundled with eslint-config-next 16) are advisory for a codebase that
    // does NOT run the React Compiler. They flag idiomatic React 19 patterns
    // (e.g. `useEffect(() => setMounted(true), [])` for hydration-safe mount,
    // ref reads in effects). Keep them visible as warnings rather than failing
    // CI on a wholesale refactor; the long-standing correctness rules
    // (exhaustive-deps, rules-of-hooks, jsx-a11y) remain errors.
    rules: {
      "react-hooks/set-state-in-effect": "warn",
      "react-hooks/immutability": "warn",
      "react-hooks/preserve-manual-memoization": "warn",
      "react-hooks/purity": "warn",
      "react-hooks/refs": "warn",
    },
  },
];

export default config;
