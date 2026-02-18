"use client";

import { useState, useCallback, useEffect, useRef, useMemo } from "react";
import dynamic from "next/dynamic";
import type { Pipeline } from "@squat-collective/rat-client";
import { ValidationError } from "@squat-collective/rat-client";
import {
  useFileTree,
  useFileContent,
  useLandingZones,
  useQuerySchema,
  useDeleteQualityTest,
} from "@/hooks/use-api";
import { useSWRConfig } from "swr";
import { KEYS } from "@/lib/cache-keys";
import { useSaveFile, detectLanguage, type OpenTab } from "@/hooks/use-editor";
import { usePreview } from "@/hooks/use-preview";
import { useApiClient } from "@/providers/api-provider";
import { Loading } from "@/components/loading";
import { FileTree } from "@/components/file-tree";
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
  DialogTrigger,
  DialogClose,
} from "@/components/ui/dialog";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import { PreviewPanel } from "@/components/preview-panel";
import { QualityTestDialog } from "@/components/quality-test-dialog";
import { Play, Save, Upload, X } from "lucide-react";

const CodeEditor = dynamic(
  () => import("@/components/code-editor").then((m) => m.CodeEditor),
  { ssr: false, loading: () => <Loading text="Loading editor..." /> },
);

interface PipelineEditorProps {
  pipeline: Pipeline;
  onVersionsRefresh: () => Promise<void>;
  triggerGlitch: () => void;
}

export function PipelineEditor({
  pipeline,
  onVersionsRefresh,
  triggerGlitch,
}: PipelineEditorProps) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const pipelinePrefix = `${pipeline.namespace}/pipelines/${pipeline.layer}/${pipeline.name}/`;

  // --- Data hooks ---
  const { data: filesData } = useFileTree(pipelinePrefix.slice(0, -1));
  const { data: landingZonesData } = useLandingZones({ namespace: pipeline.namespace });
  const allZoneNames = useMemo(
    () => landingZonesData?.zones?.map((z) => z.name) ?? [],
    [landingZonesData],
  );
  const { data: querySchema } = useQuerySchema();

  // Strip the pipeline S3 prefix from file paths for cleaner display in FileTree
  const pipelineFiles = useMemo(() => {
    if (!filesData?.files) return undefined;
    return filesData.files.map((f) => ({
      ...f,
      path: f.path.startsWith(pipelinePrefix)
        ? f.path.slice(pipelinePrefix.length)
        : f.path,
    }));
  }, [filesData, pipelinePrefix]);

  // --- Editor state ---
  const { save, saving } = useSaveFile();
  const [tabs, setTabs] = useState<OpenTab[]>([]);
  const [activeFile, setActiveFile] = useState<string | null>(null);
  const handleSaveRef = useRef<() => void>(() => {});
  const handlePreviewRef = useRef<() => void>(() => {});
  const [previewHeight, setPreviewHeight] = useState(250);
  const [showPreview, setShowPreview] = useState(false);
  const [qualityDialogOpen, setQualityDialogOpen] = useState(false);
  const [contextMenuFile, setContextMenuFile] = useState<string | null>(null);
  const { deleteTest } = useDeleteQualityTest(pipeline.namespace, pipeline.layer, pipeline.name);
  const [publishing, setPublishing] = useState(false);
  const [publishDialogOpen, setPublishDialogOpen] = useState(false);
  const [publishMessage, setPublishMessage] = useState("");
  const [publishErrors, setPublishErrors] = useState<{
    valid: boolean;
    files: Array<{
      path: string;
      valid: boolean;
      errors: string[];
      warnings: string[];
    }>;
  } | null>(null);

  // Quality test detection
  const isQualityTest = (activeFile?.includes("/tests/quality/") && activeFile.endsWith(".sql")) || false;
  const qualityTestName = isQualityTest
    ? activeFile!.split("/").pop()?.replace(/\.sql$/, "") ?? ""
    : undefined;

  // Returns the current editor content for the main pipeline file (unsaved preview).
  // When editing a quality test, returns the test SQL instead.
  const getCode = useCallback(() => {
    if (!pipeline) return undefined;

    if (isQualityTest) {
      // When editing a quality test, preview the test SQL
      const tab = tabs.find((t) => t.path === activeFile);
      return tab?.content; // always send (test has no "published" version)
    }

    // Default: main pipeline file
    const mainFile = `${pipelinePrefix}pipeline.${pipeline.type === "python" ? "py" : "sql"}`;
    const tab = tabs.find((t) => t.path === mainFile);
    if (!tab) return undefined;
    // Only send code if the tab is dirty (unsaved changes)
    return tab.content !== tab.originalContent ? tab.content : undefined;
  }, [pipeline, pipelinePrefix, tabs, activeFile, isQualityTest]);

  // Preview hook
  const preview = usePreview({
    ns: pipeline.namespace,
    layer: pipeline.layer,
    name: pipeline.name,
    getCode,
  });

  const currentTab = tabs.find((t) => t.path === activeFile);
  const fileDirty = currentTab
    ? currentTab.content !== currentTab.originalContent
    : false;

  const { data: fileData } = useFileContent(activeFile);

  useEffect(() => {
    if (!fileData || !activeFile) return;
    setTabs((prev) => {
      const existing = prev.find((t) => t.path === activeFile);
      if (existing) return prev;
      return [
        ...prev,
        {
          path: activeFile,
          content: fileData.content,
          originalContent: fileData.content,
          language: detectLanguage(activeFile),
        },
      ];
    });
  }, [fileData, activeFile]);

  const handleSelectFile = useCallback(
    (relativePath: string) => {
      setActiveFile(pipelinePrefix + relativePath);
    },
    [pipelinePrefix],
  );

  const handleCloseTab = useCallback((path: string) => {
    setTabs((prev) => {
      const remaining = prev.filter((t) => t.path !== path);
      setActiveFile((current) =>
        current === path ? (remaining[0]?.path ?? null) : current,
      );
      return remaining;
    });
  }, []);

  const handleContentChange = useCallback(
    (newContent: string) => {
      setTabs((prev) =>
        prev.map((t) =>
          t.path === activeFile ? { ...t, content: newContent } : t,
        ),
      );
    },
    [activeFile],
  );

  const handleSaveFile = useCallback(async () => {
    if (!currentTab || !fileDirty) return;
    try {
      await save(currentTab.path, currentTab.content);
      setTabs((prev) =>
        prev.map((t) =>
          t.path === currentTab.path
            ? { ...t, originalContent: t.content }
            : t,
        ),
      );
    } catch (e) {
      console.error("Failed to save file:", e);
      triggerGlitch();
    }
  }, [currentTab, fileDirty, save, triggerGlitch]);

  handleSaveRef.current = handleSaveFile;

  const handlePublish = useCallback(async (message?: string) => {
    setPublishing(true);
    setPublishErrors(null);
    try {
      await api.pipelines.publish(pipeline.namespace, pipeline.layer, pipeline.name, message);
      await mutate(KEYS.match.pipelines);
      await mutate(KEYS.match.lineage);
      await onVersionsRefresh();
      setPublishDialogOpen(false);
      setPublishMessage("");
    } catch (e) {
      if (e instanceof ValidationError && e.details) {
        setPublishErrors(e.details as typeof publishErrors);
      } else {
        console.error("Failed to publish pipeline:", e);
        triggerGlitch();
      }
    } finally {
      setPublishing(false);
    }
  }, [api, pipeline.namespace, pipeline.layer, pipeline.name, mutate, triggerGlitch, onVersionsRefresh]);

  // Wire preview: trigger + auto-preview on save
  const handleTriggerPreview = useCallback(() => {
    setShowPreview(true);
    preview.trigger();
  }, [preview]);

  handlePreviewRef.current = handleTriggerPreview;

  // Auto-preview on save
  const prevSavingRef = useRef(saving);
  useEffect(() => {
    if (prevSavingRef.current && !saving && preview.autoPreview) {
      preview.trigger();
      setShowPreview(true);
    }
    prevSavingRef.current = saving;
  }, [saving, preview]);

  return (
    <>
      <div className="flex flex-col h-[calc(100vh-180px)] border border-border/50">
        <div className="flex flex-1 min-h-0">
          {/* File tree sidebar */}
          <ContextMenu>
            <ContextMenuTrigger asChild>
              <div className="w-56 border-r border-border/50 overflow-y-auto bg-card/50">
                <div className="px-2 py-2 border-b border-border/50">
                  <p className="text-[10px] font-bold tracking-wider text-muted-foreground">
                    Files
                  </p>
                </div>
                {pipelineFiles && (
                  <FileTree
                    files={pipelineFiles}
                    onSelect={handleSelectFile}
                    onContextMenu={setContextMenuFile}
                    selectedPath={
                      activeFile?.startsWith(pipelinePrefix)
                        ? activeFile.slice(pipelinePrefix.length)
                        : activeFile
                    }
                  />
                )}
              </div>
            </ContextMenuTrigger>
            <ContextMenuContent>
              <ContextMenuItem
                onClick={() => setQualityDialogOpen(true)}
              >
                {"\uD83E\uDDEA"} Add Quality Test
              </ContextMenuItem>
              {contextMenuFile?.startsWith("tests/quality/") && contextMenuFile.endsWith(".sql") && (
                <>
                  <ContextMenuSeparator />
                  <ContextMenuItem
                    className="text-destructive focus:text-destructive"
                    onClick={async () => {
                      const testName = contextMenuFile
                        .replace("tests/quality/", "")
                        .replace(/\.sql$/, "");
                      try {
                        await deleteTest(testName);
                        await mutate(KEYS.match.files);
                      } catch (e) {
                        console.error("Failed to delete quality test:", e);
                        triggerGlitch();
                      }
                    }}
                  >
                    {"\uD83D\uDDD1\uFE0F"} Delete Test &ldquo;{contextMenuFile?.replace("tests/quality/", "").replace(/\.sql$/, "")}&rdquo;
                  </ContextMenuItem>
                </>
              )}
            </ContextMenuContent>
          </ContextMenu>

          {/* Editor + Preview vertical split */}
          <div className="flex-1 flex flex-col min-h-0 min-w-0">
            {/* File tabs */}
            {tabs.length > 0 && (
              <div className="flex border-b border-border/50 bg-card/50 overflow-x-auto">
                {tabs.map((tab) => (
                  <div
                    key={tab.path}
                    className={cn(
                      "flex items-center gap-1 px-3 py-1.5 text-[10px] border-r border-border/50 cursor-pointer",
                      tab.path === activeFile
                        ? "bg-background text-foreground"
                        : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
                    )}
                  >
                    <button
                      onClick={() => setActiveFile(tab.path)}
                      className="truncate max-w-[120px]"
                    >
                      {tab.content !== tab.originalContent && (
                        <span className="text-primary mr-1">
                          {"\u2022"}
                        </span>
                      )}
                      {tab.path.split("/").pop()}
                    </button>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        handleCloseTab(tab.path);
                      }}
                      className="hover:text-destructive"
                    >
                      <X className="h-2.5 w-2.5" />
                    </button>
                  </div>
                ))}
              </div>
            )}

            {/* CodeMirror editor */}
            <div className="flex-1 min-h-0">
              {currentTab ? (
                <CodeEditor
                  content={currentTab.originalContent}
                  language={currentTab.language}
                  activeTab={activeFile}
                  onContentChange={handleContentChange}
                  onSaveRef={handleSaveRef}
                  onPreviewRef={handlePreviewRef}
                  landingZones={allZoneNames}
                  schema={querySchema}
                />
              ) : (
                <div className="flex-1 flex items-center justify-center text-xs text-muted-foreground h-full">
                  Select a file to edit
                </div>
              )}
            </div>

            {/* Bottom status bar */}
            {currentTab && (
              <div className="flex items-center justify-between border-t border-border/50 bg-card/50 px-3 py-1">
                <span className="text-[10px] text-muted-foreground font-mono truncate">
                  {currentTab.path}
                </span>
                <div className="flex items-center gap-3">
                  <span className="text-[10px] text-muted-foreground">
                    {currentTab.language}
                  </span>
                  <Button
                    size="sm"
                    variant={showPreview ? "secondary" : "ghost"}
                    onClick={() => {
                      if (!showPreview) {
                        setShowPreview(true);
                        preview.trigger();
                      } else {
                        setShowPreview(false);
                      }
                    }}
                    className="h-6 text-[10px] gap-1"
                  >
                    <Play className="h-3 w-3" />
                    Preview
                  </Button>
                  <Dialog open={publishDialogOpen} onOpenChange={(open) => { setPublishDialogOpen(open); if (!open) setPublishErrors(null); }}>
                    <DialogTrigger asChild>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-6 text-[10px] gap-1"
                      >
                        <Upload className="h-3 w-3" />
                        Publish
                      </Button>
                    </DialogTrigger>
                    <DialogContent>
                      <DialogHeader>
                        <DialogTitle>Publish pipeline</DialogTitle>
                        <DialogDescription>
                          Snapshot current files as a new published version.
                        </DialogDescription>
                      </DialogHeader>
                      <div className="space-y-2">
                        <Label htmlFor="publish-message" className="text-[10px] tracking-wider">
                          Message (optional)
                        </Label>
                        <Input
                          id="publish-message"
                          value={publishMessage}
                          onChange={(e) => setPublishMessage(e.target.value)}
                          placeholder="e.g. Fixed join condition"
                          className="text-xs h-8"
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              handlePublish(publishMessage || undefined);
                            }
                          }}
                        />
                      </div>
                      {publishErrors && (
                        <div className="space-y-2 max-h-48 overflow-y-auto">
                          <p className="text-xs font-medium text-destructive">Template validation failed:</p>
                          {publishErrors.files?.filter((f) => !f.valid || f.warnings?.length > 0).map((f) => (
                            <div key={f.path} className="text-[10px] space-y-0.5 border border-destructive/20 p-2">
                              <p className="font-mono text-muted-foreground">{f.path.split("/").pop()}</p>
                              {f.errors?.map((err, i) => (
                                <p key={`e-${i}`} className="text-destructive">&#x2718; {err}</p>
                              ))}
                              {f.warnings?.map((warn, i) => (
                                <p key={`w-${i}`} className="text-yellow-500">&#x26A0; {warn}</p>
                              ))}
                            </div>
                          ))}
                        </div>
                      )}
                      <DialogFooter>
                        <DialogClose asChild>
                          <Button variant="ghost" size="sm">
                            Cancel
                          </Button>
                        </DialogClose>
                        <Button
                          size="sm"
                          onClick={() => handlePublish(publishMessage || undefined)}
                          disabled={publishing}
                          className="gap-1"
                        >
                          <Upload className="h-3 w-3" />
                          {publishing ? "Publishing..." : "Publish"}
                        </Button>
                      </DialogFooter>
                    </DialogContent>
                  </Dialog>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={handleSaveFile}
                    disabled={!fileDirty || saving}
                    className="h-6 text-[10px] gap-1"
                  >
                    <Save className="h-3 w-3" />
                    {saving ? "Saving..." : "Save"}
                  </Button>
                </div>
              </div>
            )}

            {/* Preview panel -- VS Code terminal style bottom panel */}
            {showPreview && (
              <div
                style={{ height: previewHeight }}
                className="shrink-0 overflow-hidden"
              >
                <PreviewPanel
                  data={preview.data}
                  loading={preview.loading}
                  error={preview.error}
                  onTrigger={preview.trigger}
                  autoPreview={preview.autoPreview}
                  onAutoPreviewChange={preview.setAutoPreview}
                  limit={preview.limit}
                  onLimitChange={preview.setLimit}
                  isQualityTest={isQualityTest}
                  qualityTestName={qualityTestName}
                />
              </div>
            )}
          </div>
        </div>
      </div>

      <QualityTestDialog
        ns={pipeline.namespace}
        layer={pipeline.layer}
        name={pipeline.name}
        open={qualityDialogOpen}
        onOpenChange={setQualityDialogOpen}
      />
    </>
  );
}
