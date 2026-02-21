"use client";

import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { RunnerPluginList } from "@/components/runner-plugin-list";

export default function PluginManagementPage() {
  return (
    <div className="space-y-6 max-w-3xl">
      <div className="flex items-center gap-3">
        <Link
          href="/settings"
          className="text-muted-foreground hover:text-primary transition-colors"
        >
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <h1 className="text-lg font-bold tracking-wider gradient-text">
          Runner Plugins
        </h1>
      </div>

      <p className="text-xs text-muted-foreground">
        Python packages installed in the runner container. These are discovered
        automatically from entry points at startup.
      </p>

      <RunnerPluginList />
    </div>
  );
}
