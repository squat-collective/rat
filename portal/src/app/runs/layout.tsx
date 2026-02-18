import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Runs | RAT",
  description: "View pipeline execution history and live run logs",
};

export default function RunsLayout({ children }: { children: React.ReactNode }) {
  return children;
}
