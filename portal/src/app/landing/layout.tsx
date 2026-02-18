import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Landing Zones | RAT",
  description: "Manage file upload landing zones for data ingestion",
};

export default function LandingLayout({ children }: { children: React.ReactNode }) {
  return children;
}
