import nextra from "nextra";

const withNextra = nextra({
  theme: "nextra-theme-docs",
  themeConfig: "./theme.config.tsx",
  defaultShowCopyCode: true,
  staticImage: true,
});

// basePath is required for GitHub Pages deployment at /rat/
const isGHPages = process.env.GITHUB_ACTIONS === "true";

export default withNextra({
  output: "export",
  images: { unoptimized: true },
  reactStrictMode: true,
  ...(isGHPages && {
    basePath: "/rat",
  }),
});
