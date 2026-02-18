import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Query | RAT",
  description: "Interactive SQL query editor powered by DuckDB",
};

export default function QueryLayout({ children }: { children: React.ReactNode }) {
  return children;
}
