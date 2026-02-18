"use client";

import { useState, useCallback } from "react";
import { useSWRConfig } from "swr";
import { useCreateQualityTest } from "@/hooks/use-api";
import { KEYS } from "@/lib/cache-keys";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogClose,
} from "@/components/ui/dialog";
import { Plus } from "lucide-react";
import { validateName } from "@/lib/validation";

const TEMPLATE_SQL = `-- @severity: error
-- @description: Describe what this test checks
SELECT *
FROM {{ ref('table_name') }}
WHERE 1 = 0`;

interface QualityTestDialogProps {
  ns: string;
  layer: string;
  name: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialSql?: string;
}

export function QualityTestDialog({
  ns,
  layer,
  name,
  open,
  onOpenChange,
  initialSql,
}: QualityTestDialogProps) {
  const { create, creating } = useCreateQualityTest(ns, layer, name);
  const { mutate } = useSWRConfig();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [testName, setTestName] = useState("");

  const testNameError = validateName(testName.trim());

  const sqlBody = initialSql
    ? `-- @severity: error\n-- @description: Describe what this test checks\n${initialSql}`
    : TEMPLATE_SQL;

  const handleCreate = useCallback(async () => {
    if (!testName.trim()) return;
    try {
      await create({
        name: testName.trim(),
        sql: sqlBody,
        severity: "error",
        description: "Describe what this test checks",
      });
      await mutate(KEYS.match.files);
      onOpenChange(false);
      setTestName("");
    } catch (e) {
      console.error("Failed to create quality test:", e);
      triggerGlitch();
    }
  }, [create, testName, sqlBody, onOpenChange, triggerGlitch]);

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
        if (!v) setTestName("");
      }}
    >
      <GlitchOverlay />
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New Quality Test</DialogTitle>
          <DialogDescription>
            Create a SQL test to validate your pipeline&apos;s output data. Tests that return rows indicate violations.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <Label htmlFor="quality-test-name" className="text-[10px] tracking-wider">Test Name</Label>
            <Input
              id="quality-test-name"
              value={testName}
              onChange={(e) => setTestName(e.target.value)}
              placeholder="e.g. no_null_ids"
              className="text-xs h-8"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleCreate();
              }}
            />
            {testNameError ? (
              <p className="text-[10px] text-destructive">{testNameError}</p>
            ) : (
              <p className="text-[10px] text-muted-foreground">
                Use snake_case. Will be saved as {testName || "test_name"}.sql
              </p>
            )}
          </div>
          {initialSql && (
            <div className="space-y-1">
              <Label htmlFor="quality-test-sql-preview" className="text-[10px] tracking-wider">SQL (from selection)</Label>
              <pre className="text-[11px] font-mono bg-background border border-border/50 p-3 max-h-[120px] overflow-auto whitespace-pre-wrap">
                {initialSql}
              </pre>
            </div>
          )}
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="ghost" size="sm">Cancel</Button>
          </DialogClose>
          <Button
            size="sm"
            onClick={handleCreate}
            disabled={creating || !testName.trim() || !!testNameError}
            className="gap-1"
          >
            <Plus className="h-3 w-3" />
            {creating ? "Creating..." : "Create Test"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
