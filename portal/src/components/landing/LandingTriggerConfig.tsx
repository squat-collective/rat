"use client";

import { useCallback, useEffect, useState } from "react";
import useSWR from "swr";
import { useApiClient } from "@/providers/api-provider";
import { KEYS } from "@/lib/cache-keys";
import { Button } from "@/components/ui/button";
import { ErrorAlert } from "@/components/error-alert";
import { cn } from "@/lib/utils";
import { ChevronDown, ChevronRight, Save, Timer } from "lucide-react";

interface LandingTriggerConfigProps {
  ns: string;
  name: string;
  onError: () => void;
}

export function LandingTriggerConfig({ ns, name, onError }: LandingTriggerConfigProps) {
  const api = useApiClient();

  const [lifecycleOpen, setLifecycleOpen] = useState(false);
  const [autoPurge, setAutoPurge] = useState(false);
  const [processedMaxAge, setProcessedMaxAge] = useState<string>("");
  const [lifecycleSaving, setLifecycleSaving] = useState(false);
  const [lifecycleLoaded, setLifecycleLoaded] = useState(false);

  const { data: lifecycleData, mutate: mutateLifecycle, error: lifecycleError } = useSWR(
    ns && name ? KEYS.zoneLifecycle(ns, name) : null,
    () => api.retention.getZoneLifecycle(ns, name),
  );

  // Sync lifecycle form from API data
  useEffect(() => {
    if (lifecycleData && !lifecycleLoaded) {
      setAutoPurge(lifecycleData.auto_purge);
      setProcessedMaxAge(
        lifecycleData.processed_max_age_days != null
          ? String(lifecycleData.processed_max_age_days)
          : "",
      );
      setLifecycleLoaded(true);
    }
  }, [lifecycleData, lifecycleLoaded]);

  const handleSaveLifecycle = useCallback(async () => {
    setLifecycleSaving(true);
    try {
      await api.retention.updateZoneLifecycle(ns, name, {
        auto_purge: autoPurge,
        processed_max_age_days: processedMaxAge
          ? Number(processedMaxAge)
          : undefined,
      });
      await mutateLifecycle();
    } catch (e) {
      console.error("Failed to save landing zone lifecycle config:", e);
      onError();
    } finally {
      setLifecycleSaving(false);
    }
  }, [api, ns, name, autoPurge, processedMaxAge, mutateLifecycle, onError]);

  return (
    <div className="border-2 border-border/50">
      <button
        type="button"
        className="w-full flex items-center gap-2 px-3 py-2.5 text-left hover:bg-muted/30 transition-colors"
        onClick={() => setLifecycleOpen(!lifecycleOpen)}
      >
        {lifecycleOpen ? (
          <ChevronDown className="h-3 w-3 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3 w-3 text-muted-foreground" />
        )}
        <Timer className="h-3 w-3 text-muted-foreground" />
        <span className="text-[10px] font-bold tracking-wider">
          Lifecycle
        </span>
        <span className="text-[9px] text-muted-foreground">
          &middot; Auto-purge processed files
        </span>
        {autoPurge && (
          <span className="ml-auto text-[9px] text-primary">enabled</span>
        )}
      </button>
      {lifecycleOpen && (
        <div className="border-t border-border/30 p-3 space-y-3">
          {lifecycleError && (
            <ErrorAlert error={lifecycleError} prefix="Failed to load lifecycle config" />
          )}
          <div className="flex items-center gap-3">
            <label htmlFor="landing-zone-auto-purge" className="text-[10px] font-bold tracking-wider text-muted-foreground">
              Auto-Purge
            </label>
            <button
              id="landing-zone-auto-purge"
              type="button"
              role="switch"
              aria-checked={autoPurge}
              onClick={() => setAutoPurge(!autoPurge)}
              className={cn(
                "relative inline-flex h-5 w-9 items-center rounded-full transition-colors",
                autoPurge ? "bg-primary" : "bg-muted-foreground/30",
              )}
            >
              <span
                className={cn(
                  "inline-block h-3.5 w-3.5 transform rounded-full bg-background transition-transform",
                  autoPurge ? "translate-x-4" : "translate-x-0.5",
                )}
              />
            </button>
            <span className="text-[10px] text-muted-foreground">
              Automatically delete processed files after max age
            </span>
          </div>
          {autoPurge && (
            <div className="space-y-1">
              <label htmlFor="landing-zone-max-age" className="text-[10px] font-bold tracking-wider text-muted-foreground">
                Processed File Max Age
              </label>
              <div className="flex items-center gap-2">
                <input
                  id="landing-zone-max-age"
                  type="number"
                  min={1}
                  value={processedMaxAge}
                  onChange={(e) => {
                    const n = Number(e.target.value);
                    if (e.target.value === "" || (!Number.isNaN(n) && n >= 1)) {
                      setProcessedMaxAge(e.target.value);
                    }
                  }}
                  placeholder="30"
                  className="w-24 bg-background border border-border/50 p-2 text-xs font-mono"
                />
                <span className="text-[10px] text-muted-foreground">
                  days
                </span>
              </div>
            </div>
          )}
          <Button
            size="sm"
            className="gap-1"
            onClick={handleSaveLifecycle}
            disabled={lifecycleSaving}
          >
            <Save className="h-3 w-3" />
            {lifecycleSaving ? "Saving..." : "Save Lifecycle"}
          </Button>
        </div>
      )}
    </div>
  );
}
