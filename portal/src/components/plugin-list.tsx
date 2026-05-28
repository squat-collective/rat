"use client";

import { useState } from "react";
import {
  usePlugins,
  useTogglePlugin,
  useRemovePlugin,
} from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Power,
  PowerOff,
  ChevronDown,
  ChevronRight,
  Trash2,
  Loader2,
} from "lucide-react";
import { PluginConfigEditor } from "@/components/plugin-config-editor";
import type { PluginEntry } from "@squat-collective/rat-client";

const statusColor: Record<string, string> = {
  registered: "text-muted-foreground border-muted-foreground/30",
  enabled: "text-primary border-primary/30",
  disabled: "text-yellow-400 border-yellow-400/30",
  error: "text-destructive border-destructive/30",
};

const healthDot = (healthy: boolean) => (
  <span
    className={`inline-block h-2 w-2 rounded-full ${healthy ? "bg-primary" : "bg-destructive"}`}
    title={healthy ? "Healthy" : "Unhealthy"}
  />
);

export function PluginList() {
  const { data: plugins, error, isLoading } = usePlugins();
  const { toggle, loading: toggling } = useTogglePlugin();
  const { remove } = useRemovePlugin();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  if (isLoading) return <Loading text="Loading plugins..." />;
  if (error) {
    triggerGlitch();
    return (
      <>
        <GlitchOverlay />
        <ErrorAlert error={error} prefix="Plugins" />
      </>
    );
  }

  if (!plugins || plugins.length === 0) {
    return (
      <div className="brutal-card p-6 text-center">
        <p className="text-xs text-muted-foreground">
          No plugins registered.
        </p>
      </div>
    );
  }

  const toggleExpand = (name: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const handleToggle = async (plugin: PluginEntry) => {
    try {
      await toggle(plugin.name, plugin.status !== "enabled");
    } catch {
      triggerGlitch();
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await remove(deleteTarget);
    } catch {
      triggerGlitch();
    } finally {
      setDeleting(false);
      setDeleteTarget(null);
    }
  };

  return (
    <>
      <GlitchOverlay />
      <div className="space-y-2">
        {plugins.map((plugin) => {
          const isExpanded = expanded.has(plugin.name);
          return (
            <div key={plugin.id} className="brutal-card">
              {/* Header row */}
              <div className="flex items-center gap-2 p-3">
                {healthDot(plugin.healthy)}
                <span className="text-xs font-bold tracking-wider">
                  {plugin.name}
                </span>
                <span className="text-[10px] text-muted-foreground font-mono">
                  v{plugin.version}
                </span>
                <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                  {plugin.kind}
                </Badge>
                <Badge
                  variant="outline"
                  className={`text-[10px] px-1.5 py-0 ${statusColor[plugin.status] ?? ""}`}
                >
                  {plugin.status}
                </Badge>

                <div className="ml-auto flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6"
                    onClick={() => handleToggle(plugin)}
                    disabled={toggling || plugin.status === "registered"}
                    title={
                      plugin.status === "enabled"
                        ? "Disable plugin"
                        : "Enable plugin"
                    }
                  >
                    {plugin.status === "enabled" ? (
                      <PowerOff className="h-3 w-3" />
                    ) : (
                      <Power className="h-3 w-3" />
                    )}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6"
                    onClick={() => toggleExpand(plugin.name)}
                  >
                    {isExpanded ? (
                      <ChevronDown className="h-3 w-3" />
                    ) : (
                      <ChevronRight className="h-3 w-3" />
                    )}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6 text-destructive hover:text-destructive"
                    onClick={() => setDeleteTarget(plugin.name)}
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </div>
              </div>

              {/* Error banner */}
              {plugin.error && (
                <div className="px-3 pb-2">
                  <p className="text-[10px] text-destructive font-mono">
                    {plugin.error}
                  </p>
                </div>
              )}

              {/* Expanded detail */}
              {isExpanded && (
                <div className="border-t border-border px-3 py-3 space-y-3">
                  {/* Metadata grid */}
                  <div className="grid grid-cols-2 gap-y-1 gap-x-4 text-[10px]">
                    <span className="text-muted-foreground">Address</span>
                    <span className="font-mono">{plugin.addr}</span>
                    <span className="text-muted-foreground">Registered</span>
                    <span>{new Date(plugin.registered_at).toLocaleString()}</span>
                    {plugin.enabled_at && (
                      <>
                        <span className="text-muted-foreground">Enabled</span>
                        <span>
                          {new Date(plugin.enabled_at).toLocaleString()}
                        </span>
                      </>
                    )}
                    <span className="text-muted-foreground">Updated</span>
                    <span>{new Date(plugin.updated_at).toLocaleString()}</span>
                  </div>

                  {/* Descriptor info */}
                  {plugin.descriptor && (
                    <DescriptorInfo descriptor={plugin.descriptor} />
                  )}

                  {/* Config editor */}
                  <PluginConfigEditor
                    name={plugin.name}
                    descriptor={plugin.descriptor}
                    currentConfig={
                      (plugin.config as Record<string, unknown>) ?? {}
                    }
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Delete confirmation dialog */}
      <Dialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove Plugin</DialogTitle>
            <DialogDescription>
              Are you sure you want to remove{" "}
              <span className="font-bold text-foreground">{deleteTarget}</span>?
              This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteTarget(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deleting}
            >
              {deleting ? (
                <Loader2 className="h-3 w-3 animate-spin mr-1" />
              ) : null}
              Remove
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function DescriptorInfo({
  descriptor,
}: {
  descriptor: Record<string, unknown>;
}) {
  const routes = descriptor.routes as
    | Array<{ method: string; path: string }>
    | undefined;
  const events = descriptor.events as string[] | undefined;

  if (!routes?.length && !events?.length) return null;

  return (
    <div className="space-y-2">
      {routes && routes.length > 0 && (
        <div>
          <h4 className="text-[10px] font-bold tracking-wider text-muted-foreground mb-1">
            Routes
          </h4>
          <div className="space-y-0.5">
            {routes.map((r, i) => (
              <div key={i} className="text-[10px] font-mono">
                <span className="text-primary">{r.method}</span>{" "}
                <span>{r.path}</span>
              </div>
            ))}
          </div>
        </div>
      )}
      {events && events.length > 0 && (
        <div>
          <h4 className="text-[10px] font-bold tracking-wider text-muted-foreground mb-1">
            Events
          </h4>
          <div className="flex flex-wrap gap-1">
            {events.map((e) => (
              <Badge
                key={e}
                variant="outline"
                className="text-[10px] px-1.5 py-0"
              >
                {e}
              </Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
