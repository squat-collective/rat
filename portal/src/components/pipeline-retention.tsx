"use client";

import { useState, useEffect, useCallback } from "react";
import useSWR, { useSWRConfig } from "swr";
import { useApiClient } from "@/providers/api-provider";
import { KEYS } from "@/lib/cache-keys";
import { useScreenGlitch } from "@/components/screen-glitch";
import { ErrorAlert } from "@/components/error-alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Save, Loader2, RotateCcw } from "lucide-react";
import type { RetentionConfig } from "@squat-collective/rat-client";

interface PipelineRetentionProps {
  ns: string;
  layer: string;
  name: string;
}

/** Fields that make sense as per-pipeline overrides. */
const OVERRIDE_FIELDS: {
  key: keyof RetentionConfig;
  label: string;
  unit: string;
}[] = [
  { key: "runs_max_per_pipeline", label: "Max Runs", unit: "runs" },
  { key: "runs_max_age_days", label: "Max Run Age", unit: "days" },
  { key: "logs_max_age_days", label: "Log Retention", unit: "days" },
  { key: "quality_results_max_per_test", label: "Quality Results", unit: "results" },
  { key: "iceberg_snapshot_max_age_days", label: "Snapshot Max Age", unit: "days" },
  { key: "iceberg_orphan_file_max_age_days", label: "Orphan File Max Age", unit: "days" },
];

export function PipelineRetention({ ns, layer, name }: PipelineRetentionProps) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const { triggerGlitch } = useScreenGlitch();

  const { data, isLoading, error } = useSWR(
    ns && layer && name
      ? KEYS.pipelineRetention(ns, layer, name)
      : null,
    () => api.retention.getPipelineRetention(ns, layer, name),
  );

  const [overrides, setOverrides] = useState<
    Record<string, number | undefined>
  >({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (data?.overrides) {
      const o: Record<string, number | undefined> = {};
      for (const f of OVERRIDE_FIELDS) {
        const val = (data.overrides as Record<string, unknown>)[f.key];
        o[f.key] = typeof val === "number" ? val : undefined;
      }
      setOverrides(o);
    }
  }, [data]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    try {
      // Build partial config — only include fields that have values
      const config: Record<string, number> = {};
      for (const [key, val] of Object.entries(overrides)) {
        if (val !== undefined) config[key] = val;
      }
      await api.retention.updatePipelineRetention(
        ns,
        layer,
        name,
        config as Partial<RetentionConfig>,
      );
      await mutate(KEYS.pipelineRetention(ns, layer, name));
    } catch (e) {
      console.error("Failed to save retention overrides:", e);
      triggerGlitch();
    } finally {
      setSaving(false);
    }
  }, [api, ns, layer, name, overrides, mutate, triggerGlitch]);

  const clearField = (key: string) => {
    setOverrides((prev) => ({ ...prev, [key]: undefined }));
  };

  if (isLoading) {
    return (
      <div className="brutal-card bg-card p-4">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          Loading retention config...
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="brutal-card bg-card p-4">
        <ErrorAlert error={error} prefix="Failed to load retention overrides" />
      </div>
    );
  }

  if (!data) return null;

  return (
    <div className="brutal-card bg-card p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Retention Overrides
        </h2>
        <Badge variant="outline" className="text-[9px]">
          Per-pipeline
        </Badge>
      </div>

      <p className="text-[10px] text-muted-foreground">
        Override system defaults for this pipeline. Clear a field to use the
        system default.
      </p>

      <div className="space-y-3">
        {OVERRIDE_FIELDS.map((f) => {
          const systemVal = data.system[f.key];
          const effectiveVal = data.effective[f.key];
          const overrideVal = overrides[f.key];
          const hasOverride = overrideVal !== undefined;

          return (
            <div key={f.key} className="grid grid-cols-[1fr_auto_auto] gap-2 items-center">
              <div className="space-y-0.5">
                <Label htmlFor={`retention-${f.key}`} className="text-[10px] tracking-wider">{f.label}</Label>
                <div className="flex items-center gap-2">
                  <Input
                    id={`retention-${f.key}`}
                    type="number"
                    min={0}
                    value={overrideVal ?? ""}
                    placeholder={String(systemVal)}
                    onChange={(e) => {
                      const v = e.target.value;
                      if (v === "") {
                        setOverrides((prev) => ({ ...prev, [f.key]: undefined }));
                      } else {
                        const n = Number(v);
                        setOverrides((prev) => ({
                          ...prev,
                          [f.key]: Number.isNaN(n) || n < 0 ? 0 : Math.floor(n),
                        }));
                      }
                    }}
                    className="text-xs h-7 font-mono w-24"
                  />
                  <span className="text-[10px] text-muted-foreground">
                    {f.unit}
                  </span>
                </div>
              </div>
              <span className="text-[9px] text-muted-foreground whitespace-nowrap">
                system: {systemVal}
              </span>
              {hasOverride && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 w-6 p-0"
                  onClick={() => clearField(f.key)}
                  title="Reset to system default"
                  aria-label={`Reset ${f.label} to system default`}
                >
                  <RotateCcw className="h-3 w-3" aria-hidden="true" />
                </Button>
              )}
            </div>
          );
        })}
      </div>

      <Button
        size="sm"
        onClick={handleSave}
        disabled={saving}
        className="gap-1"
      >
        {saving ? (
          <Loader2 className="h-3 w-3 animate-spin" />
        ) : (
          <Save className="h-3 w-3" />
        )}
        {saving ? "Saving..." : "Save Overrides"}
      </Button>
    </div>
  );
}
