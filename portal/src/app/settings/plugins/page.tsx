"use client";

import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { PluginList } from "@/components/plugin-list";
import { RunnerPluginList } from "@/components/runner-plugin-list";

export default function PluginManagementPage() {
  return (
    <div className="space-y-8 max-w-3xl">
      <div className="flex items-center gap-3">
        <Link
          href="/settings"
          className="text-muted-foreground hover:text-primary transition-colors"
        >
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <h1 className="text-lg font-bold tracking-wider gradient-text">
          Plugins
        </h1>
      </div>

      {/* Platform plugins — container plugins registered with ratd. Expand one
          to edit its configuration (rendered from its config_schema_json). */}
      <section className="space-y-3">
        <div>
          <h2 className="text-sm font-bold tracking-wider">Platform Plugins</h2>
          <p className="text-xs text-muted-foreground">
            Container plugins registered with ratd (gRPC + portal UI). Expand a
            plugin to see its details and edit its configuration.
          </p>
        </div>
        <PluginList />
      </section>

      <section className="space-y-3">
        <div>
          <h2 className="text-sm font-bold tracking-wider">Runner Plugins</h2>
          <p className="text-xs text-muted-foreground">
            Python packages installed in the runner container. Discovered
            automatically from entry points at startup.
          </p>
        </div>
        <RunnerPluginList />
      </section>
    </div>
  );
}
