"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter } from "next/navigation";
import type { Pipeline } from "@squat-collective/rat-client";
import { useUpdatePipeline, useFileContent } from "@/hooks/use-api";
import { useApiClient } from "@/providers/api-provider";
import { useSWRConfig } from "swr";
import { KEYS } from "@/lib/cache-keys";
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
import { PipelineTriggers } from "@/components/pipeline-triggers";
import { PipelineMergeStrategy } from "@/components/pipeline-merge-strategy";
import { PipelineRetention } from "@/components/pipeline-retention";
import { Save, Trash2 } from "lucide-react";

interface PipelineSettingsProps {
  pipeline: Pipeline;
  triggerGlitch: () => void;
}

export function PipelineSettings({
  pipeline,
  triggerGlitch,
}: PipelineSettingsProps) {
  const router = useRouter();
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const { update, updating } = useUpdatePipeline(
    pipeline.namespace,
    pipeline.layer,
    pipeline.name,
  );

  // Read pipeline source for merge strategy
  const pipelinePrefix = `${pipeline.namespace}/pipelines/${pipeline.layer}/${pipeline.name}/`;
  const sourceFile = `${pipelinePrefix}pipeline.${pipeline.type === "python" ? "py" : "sql"}`;
  const { data: sourceData } = useFileContent(sourceFile);

  // --- Metadata form state ---
  const [description, setDescription] = useState("");
  const [type, setType] = useState("");
  const [owner, setOwner] = useState("");
  const [formInitialized, setFormInitialized] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  useEffect(() => {
    if (pipeline && !formInitialized) {
      setDescription(pipeline.description ?? "");
      setType(pipeline.type ?? "sql");
      setOwner(pipeline.owner ?? "");
      setFormInitialized(true);
    }
  }, [pipeline, formInitialized]);

  const formDirty =
    pipeline != null &&
    (description !== (pipeline.description ?? "") ||
      type !== (pipeline.type ?? "sql") ||
      owner !== (pipeline.owner ?? ""));

  const handleSaveMetadata = useCallback(async () => {
    try {
      await update({
        description,
        type,
        owner,
      });
    } catch (e) {
      console.error("Failed to save pipeline settings:", e);
      triggerGlitch();
    }
  }, [update, description, type, owner, triggerGlitch]);

  const handleDelete = useCallback(async () => {
    setDeleting(true);
    try {
      await api.pipelines.delete(pipeline.namespace, pipeline.layer, pipeline.name);
      // Revalidate all pipeline-related SWR keys so the list page is fresh
      await mutate(KEYS.match.pipelines);
      await mutate(KEYS.match.lineage);
      router.push("/pipelines");
    } catch (e) {
      console.error("Failed to delete pipeline:", e);
      triggerGlitch();
      setDeleting(false);
      setDeleteDialogOpen(false);
    }
  }, [api, pipeline.namespace, pipeline.layer, pipeline.name, mutate, triggerGlitch, router]);

  return (
    <div className="space-y-4">
      {/* Editable metadata */}
      <div className="brutal-card bg-card p-4 space-y-4">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Pipeline Settings
        </h2>

        <div className="space-y-3">
          <div className="space-y-1">
            <Label htmlFor="pipeline-description" className="text-[10px] tracking-wider">
              Description
            </Label>
            <Textarea
              id="pipeline-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              placeholder="Pipeline description..."
              className="text-xs"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <Label htmlFor="pipeline-type" className="text-[10px] tracking-wider">
                Type
              </Label>
              <Select value={type} onValueChange={setType}>
                <SelectTrigger id="pipeline-type" className="text-xs h-8">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="sql">SQL</SelectItem>
                  <SelectItem value="python">Python</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-1">
              <Label htmlFor="pipeline-owner" className="text-[10px] tracking-wider">
                Owner
              </Label>
              <Input
                id="pipeline-owner"
                value={owner}
                onChange={(e) => setOwner(e.target.value)}
                placeholder="Owner..."
                className="text-xs h-8"
              />
            </div>
          </div>

          <Button
            size="sm"
            onClick={handleSaveMetadata}
            disabled={!formDirty || updating}
            className="gap-1"
          >
            <Save className="h-3 w-3" />
            {updating ? "Saving..." : "Save Changes"}
          </Button>
        </div>
      </div>

      {/* Merge Strategy Settings */}
      <PipelineMergeStrategy
        ns={pipeline.namespace}
        layer={pipeline.layer}
        name={pipeline.name}
        sourceCode={sourceData?.content ?? null}
        pipelineType={pipeline.type ?? "sql"}
      />

      {/* Pipeline Retention Overrides */}
      <PipelineRetention
        ns={pipeline.namespace}
        layer={pipeline.layer}
        name={pipeline.name}
      />

      {/* Pipeline Triggers */}
      <PipelineTriggers
        ns={pipeline.namespace}
        layer={pipeline.layer}
        name={pipeline.name}
      />

      {/* Danger zone */}
      <div className="brutal-card border-destructive/50 bg-card p-4 space-y-3">
        <h2 className="text-xs font-bold tracking-wider text-destructive">
          Danger Zone
        </h2>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs font-medium">Delete this pipeline</p>
            <p className="text-[10px] text-muted-foreground">
              Once deleted, the pipeline and all its files will be permanently removed.
            </p>
          </div>
          <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
            <DialogTrigger asChild>
              <Button
                variant="destructive"
                size="sm"
                className="gap-1"
              >
                <Trash2 className="h-3 w-3" />
                Delete
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Delete pipeline</DialogTitle>
                <DialogDescription>
                  Are you sure you want to delete{" "}
                  <span className="font-bold text-foreground">
                    {pipeline.namespace}/{pipeline.layer}/{pipeline.name}
                  </span>
                  ? This action cannot be undone.
                </DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <DialogClose asChild>
                  <Button variant="ghost" size="sm">
                    Cancel
                  </Button>
                </DialogClose>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={handleDelete}
                  disabled={deleting}
                  className="gap-1"
                >
                  <Trash2 className="h-3 w-3" />
                  {deleting ? "Deleting..." : "Delete Pipeline"}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      </div>
    </div>
  );
}
