"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useApiClient } from "@/providers/api-provider";
import { useNamespaces } from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Plus } from "lucide-react";
import { mutate } from "swr";
import { validateName } from "@/lib/validation";
import { KEYS } from "@/lib/cache-keys";
import type { Layer } from "@squat-collective/rat-client";

// --- Layer-specific code templates ---

const SQL_TEMPLATES: Record<string, (name: string) => string> = {
  bronze: (name) =>
    `-- @merge_strategy: full_refresh
-- ${name} (bronze)
-- Ingestion pipeline: loads raw data from external source.
-- Modify the SELECT below to match your source system.

SELECT
    *
FROM source_table
`,
  silver: (name) =>
    `-- @merge_strategy: full_refresh
-- ${name} (silver)
-- Transformation pipeline: cleans and enriches data from bronze layer.
-- Use ref("bronze.table_name") to reference upstream tables.

SELECT
    *
FROM {{ ref("bronze.TODO_source") }}
`,
  gold: (name) =>
    `-- @merge_strategy: full_refresh
-- ${name} (gold)
-- Aggregation pipeline: builds business-level metrics and dimensions.
-- Use ref("silver.table_name") to reference upstream tables.

SELECT
    *
FROM {{ ref("silver.TODO_source") }}
`,
};

const PYTHON_TEMPLATES: Record<string, (name: string) => string> = {
  bronze: (name) =>
    `# @merge_strategy: full_refresh
"""${name} (bronze) — Ingestion pipeline."""
import duckdb


def run(conn: duckdb.DuckDBPyConnection) -> None:
    """Load raw data from external source into bronze layer."""
    # TODO: Replace with your ingestion logic
    conn.execute("""
        CREATE OR REPLACE TABLE output AS
        SELECT * FROM source_table
    """)
`,
  silver: (name) =>
    `# @merge_strategy: full_refresh
"""${name} (silver) — Transformation pipeline."""
import duckdb


def run(conn: duckdb.DuckDBPyConnection) -> None:
    """Clean and transform data from bronze layer."""
    # TODO: Replace ref("bronze.TODO_source") with your upstream table
    conn.execute("""
        CREATE OR REPLACE TABLE output AS
        SELECT
            *
        FROM bronze_TODO_source
    """)
`,
  gold: (name) =>
    `# @merge_strategy: full_refresh
"""${name} (gold) — Aggregation pipeline."""
import duckdb


def run(conn: duckdb.DuckDBPyConnection) -> None:
    """Build business-level metrics from silver layer."""
    # TODO: Replace ref("silver.TODO_source") with your upstream table
    conn.execute("""
        CREATE OR REPLACE TABLE output AS
        SELECT
            *
        FROM silver_TODO_source
    """)
`,
};

function getTemplate(type: string, layer: string, name: string): string {
  const templates = type === "python" ? PYTHON_TEMPLATES : SQL_TEMPLATES;
  return (templates[layer] ?? templates["bronze"])(name);
}

function getFileName(type: string): string {
  return type === "python" ? "pipeline.py" : "pipeline.sql";
}

// --- Component ---

const NEW_NAMESPACE_VALUE = "__new__";

export function CreatePipelineDialog() {
  const api = useApiClient();
  const router = useRouter();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const { data: namespacesData } = useNamespaces();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [namespace, setNamespace] = useState("default");
  const [newNamespace, setNewNamespace] = useState("");
  const [creatingNs, setCreatingNs] = useState(false);
  const [name, setName] = useState("");
  const [layer, setLayer] = useState<Layer>("bronze");
  const [type, setType] = useState("sql");
  const [description, setDescription] = useState("");

  const isNewNamespace = namespace === NEW_NAMESPACE_VALUE;
  const effectiveNamespace = isNewNamespace ? newNamespace : namespace;
  const nameError = validateName(name);
  const nsError = isNewNamespace ? validateName(newNamespace) : null;
  const canCreate = !!name && !!effectiveNamespace && !loading && !nameError && !nsError;

  const resetForm = () => {
    setName("");
    setDescription("");
    setLayer("bronze");
    setType("sql");
    setNewNamespace("");
    setCreatingNs(false);
    setError(null);
    // Reset namespace to first available or "default"
    const first = namespacesData?.namespaces?.[0]?.name;
    setNamespace(first ?? "default");
  };

  const handleCreate = async () => {
    if (!canCreate) return;
    setLoading(true);
    setError(null);
    try {
      // Create namespace if needed
      if (isNewNamespace) {
        await api.namespaces.create(newNamespace);
        await mutate(KEYS.namespaces());
      }

      // Create pipeline
      const result = await api.pipelines.create({
        namespace: effectiveNamespace,
        name,
        layer,
        type,
        description: description || undefined,
      });

      // Scaffold template file
      const template = getTemplate(type, layer, name);
      const fileName = getFileName(type);
      await api.storage.write(`${result.s3_path}${fileName}`, template);

      setOpen(false);
      resetForm();
      mutate(KEYS.match.lineage);
      router.push(`/pipelines/${effectiveNamespace}/${layer}/${name}?tab=code`);
    } catch (e) {
      console.error("Failed to create pipeline:", e);
      const msg =
        e instanceof Error ? e.message : "Failed to create pipeline";
      setError(msg);
      triggerGlitch();
    } finally {
      setLoading(false);
    }
  };

  const handleNamespaceChange = (value: string) => {
    setNamespace(value);
    if (value !== NEW_NAMESPACE_VALUE) {
      setCreatingNs(false);
      setNewNamespace("");
    } else {
      setCreatingNs(true);
    }
  };

  return (
    <>
      <GlitchOverlay />
      <Dialog
        open={open}
        onOpenChange={(v) => {
          setOpen(v);
          if (!v) resetForm();
        }}
      >
        <DialogTrigger asChild>
          <Button size="sm" className="gap-1">
            <Plus className="h-3 w-3" /> New Pipeline
          </Button>
        </DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Pipeline</DialogTitle>
            <DialogDescription>Define a new SQL or Python pipeline.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 pt-2">
            <div className="grid grid-cols-2 gap-3">
              <div>
                <Label htmlFor="create-pipeline-namespace" className="text-[10px]">Namespace</Label>
                <Select
                  value={namespace}
                  onValueChange={handleNamespaceChange}
                >
                  <SelectTrigger id="create-pipeline-namespace" className="text-xs">
                    <SelectValue placeholder="Select namespace" />
                  </SelectTrigger>
                  <SelectContent>
                    {namespacesData?.namespaces?.map((ns) => (
                      <SelectItem key={ns.name} value={ns.name}>
                        {ns.name}
                      </SelectItem>
                    ))}
                    <SelectItem value={NEW_NAMESPACE_VALUE}>
                      <span className="flex items-center gap-1">
                        <Plus className="h-3 w-3" /> New namespace
                      </span>
                    </SelectItem>
                  </SelectContent>
                </Select>
                {creatingNs && (
                  <>
                    <Input
                      id="create-pipeline-new-namespace"
                      value={newNamespace}
                      onChange={(e) => setNewNamespace(e.target.value)}
                      placeholder="my_namespace"
                      className="text-xs mt-2"
                      autoFocus
                    />
                    {nsError && (
                      <p className="text-[10px] text-destructive mt-1">{nsError}</p>
                    )}
                  </>
                )}
              </div>
              <div>
                <Label htmlFor="create-pipeline-name" className="text-[10px]">Name</Label>
                <Input
                  id="create-pipeline-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="my_pipeline"
                  className="text-xs"
                />
                {nameError && (
                  <p className="text-[10px] text-destructive mt-1">{nameError}</p>
                )}
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <Label htmlFor="create-pipeline-layer" className="text-[10px]">Layer</Label>
                <Select value={layer} onValueChange={(v) => setLayer(v as Layer)}>
                  <SelectTrigger id="create-pipeline-layer" className="text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="bronze">Bronze</SelectItem>
                    <SelectItem value="silver">Silver</SelectItem>
                    <SelectItem value="gold">Gold</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div>
                <Label htmlFor="create-pipeline-type" className="text-[10px]">Type</Label>
                <Select value={type} onValueChange={setType}>
                  <SelectTrigger id="create-pipeline-type" className="text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="sql">SQL</SelectItem>
                    <SelectItem value="python">Python</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div>
              <Label htmlFor="create-pipeline-description" className="text-[10px]">Description</Label>
              <Textarea
                id="create-pipeline-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What does this pipeline do?"
                className="text-xs"
                rows={2}
              />
            </div>
            {error && (
              <div className="error-block px-3 py-2 text-xs text-destructive">
                {error}
              </div>
            )}
            <Button
              onClick={handleCreate}
              disabled={!canCreate}
              className="w-full"
            >
              {loading ? "Creating..." : "Create Pipeline"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
