"use client";

import { useState, useEffect } from "react";
import { Sidebar } from "@/components/nav/sidebar";
import { LicenseBanner } from "@/components/license-banner";

const SIDEBAR_KEY = "rat-sidebar-collapsed";

export function AppShell({ children }: { children: React.ReactNode }) {
  const [collapsed, setCollapsed] = useState(false);

  useEffect(() => {
    const stored = localStorage.getItem(SIDEBAR_KEY);
    if (stored === "true") setCollapsed(true);
  }, []);

  const toggle = () => {
    setCollapsed((prev) => {
      localStorage.setItem(SIDEBAR_KEY, String(!prev));
      return !prev;
    });
  };

  return (
    <div
      className="flex h-screen"
      style={{ "--sidebar-w": collapsed ? "3.5rem" : "14rem" } as React.CSSProperties}
    >
      <Sidebar collapsed={collapsed} onToggle={toggle} />
      <main className="flex-1 overflow-auto rat-bg brick-texture relative">
        <LicenseBanner />
        <div className="relative z-10 p-6 min-h-full">{children}</div>
      </main>
    </div>
  );
}
