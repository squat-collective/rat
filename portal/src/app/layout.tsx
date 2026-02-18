import type { Metadata } from "next";
import { headers } from "next/headers";
import { JetBrains_Mono } from "next/font/google";
import { ThemeProvider } from "next-themes";
import "./globals.css";
import { ApiProvider } from "@/providers/api-provider";
import { AppShell } from "@/components/app-shell";

const jetbrains = JetBrains_Mono({ subsets: ["latin"] });

export const metadata: Metadata = {
  title: {
    default: "RAT",
    template: "%s",
  },
  description: "RAT - A self-hostable data platform. Anyone can data!",
  applicationName: "RAT",
  keywords: ["data platform", "pipelines", "ETL", "DuckDB", "Iceberg"],
  robots: { index: false, follow: false },
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const nonce = (await headers()).get("x-nonce") ?? "";

  return (
    <html lang="en" suppressHydrationWarning>
      <body className={`${jetbrains.className} noise scanlines`}>
        <ThemeProvider attribute="class" defaultTheme="dark" enableSystem nonce={nonce}>
          <ApiProvider>
            <AppShell>{children}</AppShell>
          </ApiProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
