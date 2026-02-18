"use client";

import { useState, useCallback, useMemo, useRef, useEffect } from "react";
import type {
  PipelineTrigger,
  TriggerType,
  LandingZoneUploadConfig,
  CronConfig,
  PipelineSuccessConfig,
  WebhookConfig,
  FilePatternConfig,
  CronDependencyConfig,
  Pipeline,
} from "@squat-collective/rat-client";
import {
  useTriggers,
  useCreateTrigger,
  useUpdateTrigger,
  useDeleteTrigger,
  useLandingZones,
  usePipelines,
} from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  DialogClose,
} from "@/components/ui/dialog";
import { Plus, Trash2, Zap, Copy, Check } from "lucide-react";

interface PipelineTriggersProps {
  ns: string;
  layer: string;
  name: string;
}

const TRIGGER_TYPES: { value: TriggerType; label: string }[] = [
  { value: "landing_zone_upload", label: "Landing Zone Upload" },
  { value: "cron", label: "Cron Schedule" },
  { value: "pipeline_success", label: "Pipeline Success" },
  { value: "webhook", label: "Webhook" },
  { value: "file_pattern", label: "File Pattern" },
  { value: "cron_dependency", label: "Cron + Dependency" },
];

const TYPE_COLORS: Record<TriggerType, string> = {
  landing_zone_upload: "bg-amber-500/20 text-amber-400 border-amber-500/30",
  cron: "bg-blue-500/20 text-blue-400 border-blue-500/30",
  pipeline_success: "bg-green-500/20 text-green-400 border-green-500/30",
  webhook: "bg-purple-500/20 text-purple-400 border-purple-500/30",
  file_pattern: "bg-orange-500/20 text-orange-400 border-orange-500/30",
  cron_dependency: "bg-cyan-500/20 text-cyan-400 border-cyan-500/30",
};

function formatCooldown(seconds: number): string {
  if (seconds === 0) return "none";
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  return `${Math.floor(seconds / 3600)}h`;
}

function formatTimeAgo(dateStr: string | null): string {
  if (!dateStr) return "never";
  const diff = Date.now() - new Date(dateStr).getTime();
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

/**
 * Type-safe config accessor for trigger configs. The runtime type of
 * `trigger.config` is `Record<string, unknown>`, but the shape is
 * determined by `trigger.type`. This helper narrows the config type
 * based on the discriminant field for safer access.
 */
function typedConfig<T>(config: Record<string, unknown>): T {
  return config as T;
}

function triggerConfigSummary(trigger: PipelineTrigger): string {
  switch (trigger.type) {
    case "landing_zone_upload": {
      const cfg = typedConfig<LandingZoneUploadConfig>(trigger.config);
      return `${cfg.namespace}/${cfg.zone_name}`;
    }
    case "cron": {
      const cfg = typedConfig<CronConfig>(trigger.config);
      return cfg.cron_expr;
    }
    case "pipeline_success": {
      const cfg = typedConfig<PipelineSuccessConfig>(trigger.config);
      return `${cfg.namespace}/${cfg.layer}/${cfg.pipeline}`;
    }
    case "webhook": {
      const cfg = typedConfig<WebhookConfig>(trigger.config);
      return `token:${cfg.token?.slice(0, 8)}...`;
    }
    case "file_pattern": {
      const cfg = typedConfig<FilePatternConfig>(trigger.config);
      return `${cfg.zone_name}: ${cfg.pattern}`;
    }
    case "cron_dependency": {
      const cfg = typedConfig<CronDependencyConfig>(trigger.config);
      return `${cfg.cron_expr} (${cfg.dependencies?.length ?? 0} deps)`;
    }
    default:
      return JSON.stringify(trigger.config);
  }
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [text]);

  return (
    <Button variant="ghost" size="sm" className="h-5 px-1" onClick={handleCopy} aria-label="Copy to clipboard">
      {copied ? <Check className="h-3 w-3 text-green-400" aria-hidden="true" /> : <Copy className="h-3 w-3" aria-hidden="true" />}
    </Button>
  );
}

function pipelineKey(p: Pipeline): string {
  return `${p.namespace}.${p.layer}.${p.name}`;
}

/** Autocomplete text input with pipeline suggestions (ns.layer.pipeline format). */
function PipelineAutocomplete({
  id,
  pipelines,
  value,
  onChange,
  onSelect,
  exclude,
  excludeKeys,
  placeholder,
}: {
  id?: string;
  pipelines: Pipeline[];
  value: string;
  onChange: (v: string) => void;
  /** Called when a suggestion is clicked. If not provided, `onChange` is used. */
  onSelect?: (v: string) => void;
  exclude?: string;
  /** Additional keys to exclude from suggestions (e.g. already-selected deps). */
  excludeKeys?: string[];
  placeholder?: string;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  const suggestions = useMemo(() => {
    const hidden = new Set<string>();
    if (exclude) hidden.add(exclude);
    if (excludeKeys) excludeKeys.forEach((k) => hidden.add(k));
    const all = pipelines.map(pipelineKey).filter((k) => !hidden.has(k));
    if (!value) return all;
    const lower = value.toLowerCase();
    return all.filter((k) => k.toLowerCase().includes(lower));
  }, [pipelines, value, exclude, excludeKeys]);

  return (
    <div ref={ref} className="relative">
      <Input
        id={id}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        placeholder={placeholder ?? "ns.layer.pipeline"}
        className="text-xs h-8 font-mono"
      />
      {open && suggestions.length > 0 && (
        <div className="absolute z-50 mt-1 w-full max-h-32 overflow-y-auto border border-border bg-popover shadow-md">
          {suggestions.map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => {
                if (onSelect) {
                  onSelect(s);
                } else {
                  onChange(s);
                }
                setOpen(false);
              }}
              className="w-full text-left text-[10px] px-2 py-1 font-mono text-muted-foreground hover:bg-muted/50 hover:text-foreground"
            >
              {s}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function PipelineTriggers({ ns, layer, name }: PipelineTriggersProps) {
  const { data, isLoading, error } = useTriggers(ns, layer, name);
  const { create, creating } = useCreateTrigger(ns, layer, name);
  const { update } = useUpdateTrigger(ns, layer, name);
  const { deleteTrigger } = useDeleteTrigger(ns, layer, name);
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [triggerType, setTriggerType] = useState<TriggerType>("landing_zone_upload");
  const [cooldown, setCooldown] = useState("0");

  // Landing zone upload / file pattern config
  const [zoneNs, setZoneNs] = useState(ns);
  const [zoneName, setZoneName] = useState("");
  const [filePattern, setFilePattern] = useState("");

  // Cron config
  const [cronExpr, setCronExpr] = useState("");

  // Pipeline success config (ns.layer.pipeline format)
  const [upstreamPipeline, setUpstreamPipeline] = useState("");

  // Cron dependency config
  const [cronDepExpr, setCronDepExpr] = useState("");
  const [selectedDeps, setSelectedDeps] = useState<string[]>([]);
  const [depInput, setDepInput] = useState("");

  const { data: zonesData } = useLandingZones({ namespace: zoneNs });
  const { data: allPipelinesData } = usePipelines();

  const resetForm = useCallback(() => {
    setTriggerType("landing_zone_upload");
    setCooldown("0");
    setZoneNs(ns);
    setZoneName("");
    setFilePattern("");
    setCronExpr("");
    setUpstreamPipeline("");
    setCronDepExpr("");
    setSelectedDeps([]);
    setDepInput("");
  }, [ns]);

  const canCreate = useCallback((): boolean => {
    switch (triggerType) {
      case "landing_zone_upload":
        return !!zoneName;
      case "cron":
        return !!cronExpr;
      case "pipeline_success":
        return upstreamPipeline.split(".").length === 3 && upstreamPipeline.split(".").every(Boolean);
      case "webhook":
        return true; // no config needed
      case "file_pattern":
        return !!zoneName && !!filePattern;
      case "cron_dependency":
        return !!cronDepExpr && selectedDeps.length > 0;
      default:
        return false;
    }
  }, [triggerType, zoneName, cronExpr, upstreamPipeline, filePattern, cronDepExpr, selectedDeps]);

  const handleCreate = useCallback(async () => {
    let config: Record<string, unknown> = {};

    switch (triggerType) {
      case "landing_zone_upload":
        config = { namespace: zoneNs, zone_name: zoneName };
        break;
      case "cron":
        config = { cron_expr: cronExpr };
        break;
      case "pipeline_success": {
        const [pNs, pLayer, pName] = upstreamPipeline.split(".");
        config = { namespace: pNs, layer: pLayer, pipeline: pName };
        break;
      }
      case "webhook":
        config = {}; // token auto-generated by backend
        break;
      case "file_pattern":
        config = { namespace: zoneNs, zone_name: zoneName, pattern: filePattern };
        break;
      case "cron_dependency":
        config = { cron_expr: cronDepExpr, dependencies: selectedDeps };
        break;
    }

    try {
      await create({
        type: triggerType,
        config,
        cooldown_seconds: parseInt(cooldown, 10) || 0,
      });
      setAddDialogOpen(false);
      resetForm();
    } catch (e) {
      console.error("Failed to create trigger:", e);
      triggerGlitch();
    }
  }, [create, triggerType, zoneNs, zoneName, cronExpr, upstreamPipeline, filePattern, cronDepExpr, selectedDeps, cooldown, resetForm, triggerGlitch]);

  const handleToggle = useCallback(
    async (trigger: PipelineTrigger) => {
      try {
        await update(trigger.id, { enabled: !trigger.enabled });
      } catch (e) {
        console.error("Failed to toggle trigger:", e);
        triggerGlitch();
      }
    },
    [update, triggerGlitch],
  );

  const handleDelete = useCallback(
    async (triggerId: string) => {
      try {
        await deleteTrigger(triggerId);
      } catch (e) {
        console.error("Failed to delete trigger:", e);
        triggerGlitch();
      }
    },
    [deleteTrigger, triggerGlitch],
  );

  const addDep = useCallback((dep: string) => {
    setSelectedDeps((prev) => (prev.includes(dep) ? prev : [...prev, dep]));
    setDepInput("");
  }, []);

  const removeDep = useCallback((dep: string) => {
    setSelectedDeps((prev) => prev.filter((d) => d !== dep));
  }, []);

  const triggers = data?.triggers ?? [];
  const allPipelines = allPipelinesData?.pipelines ?? [];
  const currentPipelineKey = `${ns}.${layer}.${name}`;

  return (
    <div className="brutal-card bg-card p-4 space-y-3">
      <GlitchOverlay />
      <div className="flex items-center justify-between">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Triggers
        </h2>
        <Dialog
          open={addDialogOpen}
          onOpenChange={(open) => {
            setAddDialogOpen(open);
            if (!open) resetForm();
          }}
        >
          <DialogTrigger asChild>
            <Button variant="ghost" size="sm" className="h-6 text-[10px] gap-1">
              <Plus className="h-3 w-3" />
              Add Trigger
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add Trigger</DialogTitle>
              <DialogDescription>
                Create a trigger to automatically run this pipeline on events.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              {/* Type selector */}
              <div className="space-y-1">
                <Label htmlFor="trigger-type" className="text-[10px] tracking-wider">Type</Label>
                <Select
                  value={triggerType}
                  onValueChange={(v) => setTriggerType(v as TriggerType)}
                >
                  <SelectTrigger id="trigger-type" className="text-xs h-8">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {TRIGGER_TYPES.map((t) => (
                      <SelectItem key={t.value} value={t.value}>
                        {t.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {/* Dynamic config form */}
              {triggerType === "landing_zone_upload" && (
                <div className="space-y-2">
                  <div className="space-y-1">
                    <Label htmlFor="trigger-lz-zone" className="text-[10px] tracking-wider">Landing Zone</Label>
                    <Select value={zoneName} onValueChange={setZoneName}>
                      <SelectTrigger id="trigger-lz-zone" className="text-xs h-8">
                        <SelectValue placeholder="Select a landing zone..." />
                      </SelectTrigger>
                      <SelectContent>
                        {(zonesData?.zones ?? []).map((z) => (
                          <SelectItem key={z.name} value={z.name}>
                            {z.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                </div>
              )}

              {triggerType === "cron" && (
                <div className="space-y-1">
                  <Label htmlFor="trigger-cron-expr" className="text-[10px] tracking-wider">Cron Expression</Label>
                  <Input
                    id="trigger-cron-expr"
                    value={cronExpr}
                    onChange={(e) => setCronExpr(e.target.value)}
                    placeholder="*/15 * * * *"
                    className="text-xs h-8 font-mono"
                  />
                  <p className="text-[10px] text-muted-foreground">
                    5-field cron: minute hour day-of-month month day-of-week
                  </p>
                </div>
              )}

              {triggerType === "pipeline_success" && (
                <div className="space-y-1">
                  <Label htmlFor="trigger-upstream-pipeline" className="text-[10px] tracking-wider">Upstream Pipeline</Label>
                  <PipelineAutocomplete
                    id="trigger-upstream-pipeline"
                    pipelines={allPipelines}
                    value={upstreamPipeline}
                    onChange={setUpstreamPipeline}
                    exclude={currentPipelineKey}
                    placeholder="ns.layer.pipeline"
                  />
                  <p className="text-[10px] text-muted-foreground">
                    Fires when the selected upstream pipeline completes successfully.
                  </p>
                </div>
              )}

              {triggerType === "webhook" && (
                <p className="text-[10px] text-muted-foreground">
                  A unique webhook endpoint and secret token will be generated automatically.
                  Send the token via <code className="font-mono text-[9px] bg-muted px-1">X-Webhook-Token</code> header
                  or <code className="font-mono text-[9px] bg-muted px-1">Authorization: Bearer &lt;token&gt;</code>.
                </p>
              )}

              {triggerType === "file_pattern" && (
                <div className="space-y-2">
                  <div className="space-y-1">
                    <Label htmlFor="trigger-fp-zone" className="text-[10px] tracking-wider">Landing Zone</Label>
                    <Select value={zoneName} onValueChange={setZoneName}>
                      <SelectTrigger id="trigger-fp-zone" className="text-xs h-8">
                        <SelectValue placeholder="Select a landing zone..." />
                      </SelectTrigger>
                      <SelectContent>
                        {(zonesData?.zones ?? []).map((z) => (
                          <SelectItem key={z.name} value={z.name}>
                            {z.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="trigger-file-pattern" className="text-[10px] tracking-wider">File Pattern</Label>
                    <Input
                      id="trigger-file-pattern"
                      value={filePattern}
                      onChange={(e) => setFilePattern(e.target.value)}
                      placeholder="*.csv"
                      className="text-xs h-8 font-mono"
                    />
                    <p className="text-[10px] text-muted-foreground">
                      Glob pattern to match filenames (e.g., *.csv, orders_*.parquet)
                    </p>
                  </div>
                </div>
              )}

              {triggerType === "cron_dependency" && (
                <div className="space-y-2">
                  <div className="space-y-1">
                    <Label htmlFor="trigger-cron-dep-expr" className="text-[10px] tracking-wider">Cron Expression</Label>
                    <Input
                      id="trigger-cron-dep-expr"
                      value={cronDepExpr}
                      onChange={(e) => setCronDepExpr(e.target.value)}
                      placeholder="*/15 * * * *"
                      className="text-xs h-8 font-mono"
                    />
                    <p className="text-[10px] text-muted-foreground">
                      Only fires on schedule if at least one dependency has new data.
                    </p>
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="trigger-add-dependency" className="text-[10px] tracking-wider">
                      Add Dependency ({selectedDeps.length} selected)
                    </Label>
                    <PipelineAutocomplete
                      id="trigger-add-dependency"
                      pipelines={allPipelines}
                      value={depInput}
                      onChange={setDepInput}
                      onSelect={addDep}
                      exclude={currentPipelineKey}
                      excludeKeys={selectedDeps}
                      placeholder="ns.layer.pipeline"
                    />
                    <p className="text-[10px] text-muted-foreground">
                      Select a pipeline from suggestions to add it as a dependency.
                    </p>
                    {selectedDeps.length > 0 && (
                      <div className="flex flex-wrap gap-1 mt-1">
                        {selectedDeps.map((d) => (
                          <Badge
                            key={d}
                            variant="outline"
                            className="text-[9px] bg-cyan-500/20 text-cyan-400 border-cyan-500/30 cursor-pointer"
                            onClick={() => removeDep(d)}
                            role="button"
                            aria-label={`Remove dependency ${d}`}
                          >
                            {d} &times;
                          </Badge>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* Cooldown (always shown) */}
              <div className="space-y-1">
                <Label htmlFor="trigger-cooldown" className="text-[10px] tracking-wider">
                  Cooldown (seconds)
                </Label>
                <Input
                  id="trigger-cooldown"
                  type="number"
                  min={0}
                  value={cooldown}
                  onChange={(e) => {
                    const n = Number(e.target.value);
                    setCooldown(Number.isNaN(n) || n < 0 ? "0" : e.target.value);
                  }}
                  placeholder="0"
                  className="text-xs h-8"
                />
                <p className="text-[10px] text-muted-foreground">
                  Minimum time between triggered runs. 0 = no cooldown.
                </p>
              </div>
            </div>
            <DialogFooter>
              <DialogClose asChild>
                <Button variant="ghost" size="sm">
                  Cancel
                </Button>
              </DialogClose>
              <Button
                size="sm"
                onClick={handleCreate}
                disabled={creating || !canCreate()}
                className="gap-1"
              >
                <Zap className="h-3 w-3" />
                {creating ? "Creating..." : "Create Trigger"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {isLoading && (
        <p className="text-[10px] text-muted-foreground">Loading triggers...</p>
      )}

      {error && <ErrorAlert error={error} prefix="Failed to load triggers" />}

      {!isLoading && !error && triggers.length === 0 && (
        <p className="text-[10px] text-muted-foreground">
          No triggers configured. Add a trigger to auto-run this pipeline on events.
        </p>
      )}

      {triggers.length > 0 && (
        <div className="space-y-2">
          {triggers.map((trigger) => (
            <div
              key={trigger.id}
              className="flex items-center justify-between border border-border/50 p-2"
            >
              <div className="flex items-center gap-2 min-w-0">
                <Zap className="h-3 w-3 text-muted-foreground shrink-0" />
                <Badge
                  variant="outline"
                  className={`text-[9px] shrink-0 ${TYPE_COLORS[trigger.type] ?? ""}`}
                >
                  {trigger.type.replace(/_/g, " ")}
                </Badge>
                <span className="text-[10px] font-mono truncate">
                  {triggerConfigSummary(trigger)}
                </span>
                {trigger.type === "webhook" && trigger.webhook_url && (
                  <CopyButton
                    text={
                      trigger.webhook_token
                        ? `curl -X POST ${trigger.webhook_url} -H "X-Webhook-Token: ${trigger.webhook_token}"`
                        : trigger.webhook_url
                    }
                  />
                )}
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <span className="text-[10px] text-muted-foreground">
                  cd: {formatCooldown(trigger.cooldown_seconds)}
                </span>
                <span className="text-[10px] text-muted-foreground">
                  last: {formatTimeAgo(trigger.last_triggered_at)}
                </span>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-5 px-2 text-[9px]"
                  onClick={() => handleToggle(trigger)}
                  aria-label={`${trigger.enabled ? "Disable" : "Enable"} ${trigger.type.replace(/_/g, " ")} trigger`}
                >
                  {trigger.enabled ? (
                    <Badge className="text-[9px] bg-green-500/20 text-green-400 border-green-500/30">
                      ON
                    </Badge>
                  ) : (
                    <Badge variant="secondary" className="text-[9px]">
                      OFF
                    </Badge>
                  )}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-5 w-5 p-0 text-muted-foreground hover:text-destructive"
                  onClick={() => handleDelete(trigger.id)}
                  aria-label={`Delete ${trigger.type.replace(/_/g, " ")} trigger`}
                >
                  <Trash2 className="h-3 w-3" aria-hidden="true" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
