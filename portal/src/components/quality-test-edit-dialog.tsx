"use client";

import { useCallback, useEffect, useState } from "react";
import { useSWRConfig } from "swr";
import { useSaveFile } from "@/hooks/use-editor";
import { KEYS } from "@/lib/cache-keys";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Loader2, Save } from "lucide-react";
import { ConfigCodeField } from "@/components/config-code-field";

interface QualityTestEditDialogProps {
  ns: string;
  layer: string;
  name: string;
  test: { name: string; sql: string } | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * Edit a quality test's SQL in place. The test SQL lives in
 * <ns>/pipelines/<layer>/<name>/tests/quality/<testName>.sql, so a save is
 * just a file write — no dedicated update endpoint needed.
 */
export function QualityTestEditDialog({
  ns,
  layer,
  name,
  test,
  open,
  onOpenChange,
}: QualityTestEditDialogProps) {
  const { save, saving } = useSaveFile();
  const { mutate } = useSWRConfig();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [sql, setSql] = useState("");

  useEffect(() => {
    if (test) setSql(test.sql);
  }, [test]);

  const path = test
    ? `${ns}/pipelines/${layer}/${name}/tests/quality/${test.name}.sql`
    : "";

  const handleSave = useCallback(async () => {
    if (!path) return;
    try {
      await save(path, sql);
      await mutate(KEYS.qualityTests(ns, layer, name));
      await mutate(KEYS.match.files);
      onOpenChange(false);
    } catch (e) {
      console.error("Failed to save quality test:", e);
      triggerGlitch();
    }
  }, [save, path, sql, mutate, ns, layer, name, onOpenChange, triggerGlitch]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <GlitchOverlay />
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Edit Quality Test</DialogTitle>
          <DialogDescription>
            Edit the SQL for <span className="font-mono">{test?.name}</span>.
            Rows returned by the query are treated as violations.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Label className="text-[10px] tracking-wider">SQL</Label>
          {test && (
            <ConfigCodeField value={sql} language="sql" onChange={setSql} />
          )}
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="ghost" size="sm">
              Cancel
            </Button>
          </DialogClose>
          <Button size="sm" onClick={handleSave} disabled={saving} className="gap-1">
            {saving ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Save className="h-3 w-3" />
            )}
            {saving ? "Saving..." : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
