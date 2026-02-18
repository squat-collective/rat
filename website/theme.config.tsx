import type { DocsThemeConfig } from "nextra-theme-docs";

const config: DocsThemeConfig = {
  logo: (
    <span className="font-mono font-bold text-lg tracking-tight">
      <span className="neon-text">RAT</span>{" "}
      <span className="text-muted-foreground text-sm">Docs</span>
    </span>
  ),
  project: {
    link: "https://github.com/squat-collective/rat",
  },
  docsRepositoryBase: "https://github.com/squat-collective/rat/tree/main/website",
  head: (
    <>
      <meta name="viewport" content="width=device-width, initial-scale=1.0" />
      <meta name="description" content="RAT — A self-hostable data platform. Anyone can data!" />
      <meta name="og:title" content="RAT Docs" />
      <link rel="preconnect" href="https://fonts.googleapis.com" />
      <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
      <link
        href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600;700&display=swap"
        rel="stylesheet"
      />
    </>
  ),
  sidebar: {
    defaultMenuCollapseLevel: 1,
    toggleButton: true,
  },
  toc: {
    backToTop: true,
  },
  footer: {
    content: (
      <div className="flex w-full flex-col items-center sm:items-start">
        <p className="text-xs text-muted-foreground font-mono">
          RAT — Anyone can data! Built by{" "}
          <a
            href="https://squat-collective.github.io/website/"
            target="_blank"
            rel="noopener noreferrer"
            className="underline hover:text-foreground"
          >
            Le Squat
          </a>
          .
        </p>
      </div>
    ),
  },
  darkMode: true,
};

export default config;
