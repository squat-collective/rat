"use client";

import { useState, useEffect, useCallback, useMemo } from "react";
import {
  useRetentionConfig,
  useReaperStatus,
  useUpdateRetentionConfig,
  useTriggerReaper,
} from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Save, Play, Loader2, ArrowLeft } from "lucide-react";
import Link from "next/link";
import type { RetentionConfig } from "@squat-collective/rat-client";

/** A single numeric config field. */
function ConfigField({
  label,
  description,
  value,
  onChange,
  unit,
}: {
  label: string;
  description: string;
  value: number;
  onChange: (v: number) => void;
  unit: string;
}) {
  const fieldId = `retention-${label.toLowerCase().replace(/\s+/g, "-")}`;
  return (
    <div className="space-y-1">
      <Label htmlFor={fieldId} className="text-[10px] tracking-wider">{label}</Label>
      <div className="flex items-center gap-2">
        <Input
          id={fieldId}
          type="number"
          min={0}
          value={value}
          onChange={(e) => {
            const n = Number(e.target.value);
            onChange(Number.isNaN(n) || n < 0 ? 0 : Math.floor(n));
          }}
          className="text-xs h-8 font-mono w-28"
        />
        <span className="text-[10px] text-muted-foreground">{unit}</span>
      </div>
      <p className="text-[10px] text-muted-foreground">{description}</p>
    </div>
  );
}

export default function RetentionSettingsPage() {
  const { data, isLoading, error: configError } = useRetentionConfig();
  const { data: status, mutate: mutateStatus, error: statusError } = useReaperStatus();
  const { update, updating } = useUpdateRetentionConfig();
  const { trigger: triggerReaper, running: reaperRunning } = useTriggerReaper();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  // Form state -- initialized once from server data, then managed locally.
  // The `savedConfig` ref tracks the last-saved state to compute dirty status.
  const [form, setForm] = useState<RetentionConfig | null>(null);
  const [savedConfig, setSavedConfig] = useState<RetentionConfig | null>(null);

  useEffect(() => {
    if (data?.config && !form) {
      setForm(data.config);
      setSavedConfig(data.config);
    }
  }, [data, form]);

  const setField = useCallback(
    <K extends keyof RetentionConfig>(key: K, value: RetentionConfig[K]) => {
      setForm((prev) => (prev ? { ...prev, [key]: value } : prev));
    },
    [],
  );

  /** Whether the form has pending unsaved changes. */
  const isDirty = useMemo(() => {
    if (!form || !savedConfig) return false;
    return (Object.keys(form) as Array<keyof RetentionConfig>).some(
      (k) => form[k] !== savedConfig[k],
    );
  }, [form, savedConfig]);

  const handleSave = useCallback(async () => {
    if (!form) return;
    try {
      await update(form);
      setSavedConfig(form);
    } catch (e) {
      console.error("Failed to save retention config:", e);
      triggerGlitch();
    }
  }, [form, update, triggerGlitch]);

  const handleRunNow = useCallback(async () => {
    try {
      await triggerReaper();
      await mutateStatus();
    } catch (e) {
      console.error("Failed to trigger reaper:", e);
      triggerGlitch();
    }
  }, [triggerReaper, mutateStatus, triggerGlitch]);

  if (isLoading || !form) return <Loading text="Loading retention config..." />;
  if (configError) return <ErrorAlert error={configError} prefix="Failed to load retention config" />;

  return (
    <div className="space-y-6 max-w-2xl">
      <GlitchOverlay />

      <div>
        <Link
          href="/settings"
          className="text-[10px] text-muted-foreground hover:text-primary flex items-center gap-1 mb-2"
        >
          <ArrowLeft className="h-3 w-3" /> Back to settings
        </Link>
        <h1 className="text-lg font-bold tracking-wider gradient-text">
          Data Retention
        </h1>
        <p className="text-xs text-muted-foreground mt-1">
          Configure automatic cleanup of old runs, logs, and orphan data.
        </p>
      </div>

      {/* Reaper Status */}
      <div className="brutal-card p-4 space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
            Reaper Status
          </h2>
          <Button
            size="sm"
            variant="ghost"
            className="h-7 text-[10px] gap-1"
            disabled={reaperRunning}
            onClick={handleRunNow}
          >
            {reaperRunning ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Play className="h-3 w-3" />
            )}
            {reaperRunning ? "Running..." : "Run Now"}
          </Button>
        </div>

        {statusError ? (
          <ErrorAlert error={statusError} prefix="Failed to load reaper status" />
        ) : status ? (
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                Last Run
              </p>
              <p className="text-xs font-medium mt-0.5">
                {status.last_run_at
                  ? new Date(status.last_run_at).toLocaleString()
                  : "Never"}
              </p>
            </div>
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                Runs Pruned
              </p>
              <p className="text-xs font-mono mt-0.5">{status.runs_pruned}</p>
            </div>
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                Logs Pruned
              </p>
              <p className="text-xs font-mono mt-0.5">{status.logs_pruned}</p>
            </div>
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                Quality Pruned
              </p>
              <p className="text-xs font-mono mt-0.5">
                {status.quality_pruned}
              </p>
            </div>
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                Pipelines Purged
              </p>
              <p className="text-xs font-mono mt-0.5">
                {status.pipelines_purged}
              </p>
            </div>
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                Stuck Runs Failed
              </p>
              <p className="text-xs font-mono mt-0.5">{status.runs_failed}</p>
            </div>
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                Branches Cleaned
              </p>
              <p className="text-xs font-mono mt-0.5">
                {status.branches_cleaned}
              </p>
            </div>
            <div>
              <p className="text-[10px] tracking-wider text-muted-foreground">
                LZ Files Cleaned
              </p>
              <p className="text-xs font-mono mt-0.5">
                {status.lz_files_cleaned}
              </p>
            </div>
          </div>
        ) : (
          <p className="text-[10px] text-muted-foreground">
            No reaper status available yet.
          </p>
        )}
      </div>

      {/* Run History */}
      <div className="brutal-card p-4 space-y-4">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Run History
        </h2>
        <div className="grid grid-cols-2 gap-4">
          <ConfigField
            label="Max Runs Per Pipeline"
            description="Keep the last N runs per pipeline"
            value={form.runs_max_per_pipeline}
            onChange={(v) => setField("runs_max_per_pipeline", v)}
            unit="runs"
          />
          <ConfigField
            label="Max Run Age"
            description="Delete runs older than this"
            value={form.runs_max_age_days}
            onChange={(v) => setField("runs_max_age_days", v)}
            unit="days"
          />
        </div>
      </div>

      {/* Logs & Quality */}
      <div className="brutal-card p-4 space-y-4">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Logs & Quality
        </h2>
        <div className="grid grid-cols-2 gap-4">
          <ConfigField
            label="Log Retention"
            description="Delete S3 log files older than this"
            value={form.logs_max_age_days}
            onChange={(v) => setField("logs_max_age_days", v)}
            unit="days"
          />
          <ConfigField
            label="Quality Results"
            description="Keep the last N results per test"
            value={form.quality_results_max_per_test}
            onChange={(v) => setField("quality_results_max_per_test", v)}
            unit="results"
          />
        </div>
      </div>

      {/* Operational */}
      <div className="brutal-card p-4 space-y-4">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Operational Cleanup
        </h2>
        <div className="grid grid-cols-2 gap-4">
          <ConfigField
            label="Soft Delete Purge"
            description="Hard-delete soft-deleted pipelines after"
            value={form.soft_delete_purge_days}
            onChange={(v) => setField("soft_delete_purge_days", v)}
            unit="days"
          />
          <ConfigField
            label="Stuck Run Timeout"
            description="Fail stuck pending/running runs after"
            value={form.stuck_run_timeout_minutes}
            onChange={(v) => setField("stuck_run_timeout_minutes", v)}
            unit="minutes"
          />
          <ConfigField
            label="Audit Log Retention"
            description="Prune audit log entries older than"
            value={form.audit_log_max_age_days}
            onChange={(v) => setField("audit_log_max_age_days", v)}
            unit="days"
          />
          <ConfigField
            label="Orphan Branch Max Age"
            description="Clean Nessie run-* branches older than"
            value={form.nessie_orphan_branch_max_age_hours}
            onChange={(v) => setField("nessie_orphan_branch_max_age_hours", v)}
            unit="hours"
          />
        </div>
      </div>

      {/* Iceberg Maintenance */}
      <div className="brutal-card p-4 space-y-4">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Iceberg Maintenance
        </h2>
        <p className="text-[10px] text-muted-foreground">
          Runs automatically after each successful pipeline execution.
        </p>
        <div className="grid grid-cols-2 gap-4">
          <ConfigField
            label="Snapshot Max Age"
            description="Expire Iceberg snapshots older than"
            value={form.iceberg_snapshot_max_age_days}
            onChange={(v) => setField("iceberg_snapshot_max_age_days", v)}
            unit="days"
          />
          <ConfigField
            label="Orphan File Max Age"
            description="Remove orphan data files older than"
            value={form.iceberg_orphan_file_max_age_days}
            onChange={(v) => setField("iceberg_orphan_file_max_age_days", v)}
            unit="days"
          />
        </div>
      </div>

      {/* Reaper Interval */}
      <div className="brutal-card p-4 space-y-4">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Schedule
        </h2>
        <ConfigField
          label="Reaper Interval"
          description="How often the system reaper runs"
          value={form.reaper_interval_minutes}
          onChange={(v) => setField("reaper_interval_minutes", v)}
          unit="minutes"
        />
      </div>

      {/* Save */}
      <div className="flex items-center justify-end gap-2">
        {isDirty && (
          <span className="text-[9px] text-yellow-500">Unsaved changes</span>
        )}
        <Button
          size="sm"
          onClick={handleSave}
          disabled={updating || !isDirty}
          className="gap-1"
        >
          {updating ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Save className="h-3 w-3" />
          )}
          {updating ? "Saving..." : "Save Retention Settings"}
        </Button>
      </div>
    </div>
  );
}
