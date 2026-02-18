import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Lineage | RAT",
  description: "Pipeline dependency graph and data lineage visualization",
};

export default function LineageLayout({ children }: { children: React.ReactNode }) {
  return children;
}
