"use client";

import { useCallback, useState } from "react";
import { useApiClient } from "@/providers/api-provider";
import { Button } from "@/components/ui/button";
import { FilePreviewModal } from "@/components/file-preview-modal";
import { cn, formatBytes } from "@/lib/utils";
import { Trash2, Upload } from "lucide-react";
import { mutate } from "swr";
import { KEYS } from "@/lib/cache-keys";
import { formatDate } from "./utils";
import type { LandingFile } from "@squat-collective/rat-client";

interface LandingFileManagerProps {
  ns: string;
  name: string;
  files: LandingFile[];
  onError: () => void;
}

export function LandingFileManager({ ns, name, files, onError }: LandingFileManagerProps) {
  const api = useApiClient();

  const [uploading, setUploading] = useState(false);
  const [dragOver, setDragOver] = useState(false);

  const uploadFiles = useCallback(
    async (fileList: FileList | File[]) => {
      setUploading(true);
      try {
        for (const file of Array.from(fileList)) {
          await api.landing.uploadFile(ns, name, file, file.name);
        }
        mutate(KEYS.match.landingFiles(ns, name));
        mutate(KEYS.match.landingZone(ns, name));
      } catch (e) {
        console.error("Failed to upload files:", e);
        onError();
      } finally {
        setUploading(false);
      }
    },
    [api, ns, name, onError],
  );

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragOver(false);
      if (e.dataTransfer.files.length > 0) {
        uploadFiles(e.dataTransfer.files);
      }
    },
    [uploadFiles],
  );

  const handleFileInput = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (e.target.files && e.target.files.length > 0) {
        uploadFiles(e.target.files);
        e.target.value = "";
      }
    },
    [uploadFiles],
  );

  const handleDeleteFile = useCallback(async (fileId: string) => {
    try {
      await api.landing.deleteFile(ns, name, fileId);
    } catch (e: unknown) {
      // 404 = file already gone (likely processed by a pipeline run) -- treat as success
      const isNotFound = e instanceof Error && (
        "statusCode" in e && (e as { statusCode: number }).statusCode === 404
        || e.name === "NotFoundError"
      );
      if (!isNotFound) {
        console.error("Failed to delete file:", e);
        onError();
        return;
      }
    }
    // Always refresh the list -- file is gone either way
    mutate(KEYS.match.landingFiles(ns, name));
    mutate(KEYS.match.landingZone(ns, name));
  }, [api, ns, name, onError]);

  return (
    <>
      {/* Drop Zone */}
      <div
        role="region"
        aria-label="File drop zone"
        className={cn(
          "border-2 border-dashed p-6 text-center transition-all",
          dragOver
            ? "border-primary bg-primary/10"
            : "border-border/50 hover:border-primary/50",
        )}
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={handleDrop}
      >
        <input
          id="file-upload"
          type="file"
          multiple
          className="hidden"
          aria-label="Upload files — CSV, JSON, Parquet, Excel, max 32MB per file"
          onChange={handleFileInput}
        />
        <Upload className="h-6 w-6 mx-auto mb-2 text-muted-foreground" aria-hidden="true" />
        <p className="text-xs text-muted-foreground">
          {uploading
            ? "Uploading..."
            : "Drop files here or use the button below"}
        </p>
        <p className="text-[9px] text-muted-foreground/60 mt-1">
          CSV, JSON, Parquet, Excel &middot; Max 32MB per file
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-3 text-xs"
          disabled={uploading}
          onClick={() => document.getElementById("file-upload")?.click()}
        >
          <Upload className="h-3 w-3 mr-1.5" aria-hidden="true" />
          Choose files
        </Button>
      </div>

      {/* File Table */}
      <div className="overflow-auto border-2 border-border/50">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-card/95 backdrop-blur-sm z-10">
            <tr>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30 w-8">
                #
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Filename
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Size
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Type
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Uploaded
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-right text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Actions
              </th>
            </tr>
          </thead>
          <tbody>
            {files.map((f, i) => (
              <tr
                key={f.id}
                className={cn(
                  "group border-t border-border/20 transition-all",
                  "hover:bg-primary/5",
                  i % 2 === 0 ? "bg-transparent" : "bg-muted/30",
                )}
              >
                <td className="whitespace-nowrap px-3 py-2 font-mono text-[10px] text-muted-foreground/50">
                  {(i + 1).toString().padStart(2, "0")}
                </td>
                <td className="whitespace-nowrap px-3 py-2 font-medium font-mono">
                  {f.filename}
                </td>
                <td className="whitespace-nowrap px-3 py-2 font-mono text-muted-foreground">
                  {formatBytes(f.size_bytes)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">
                  {f.content_type}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">
                  {formatDate(f.uploaded_at)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-right">
                  <div className="flex items-center justify-end gap-1">
                    <FilePreviewModal
                      s3Path={f.s3_path}
                      filename={f.filename}
                      namespace={ns}
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 px-2 text-[10px] text-destructive hover:text-destructive"
                      onClick={() => handleDeleteFile(f.id)}
                      aria-label={`Delete ${f.filename}`}
                    >
                      <Trash2 className="h-3 w-3" aria-hidden="true" />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {files.length === 0 && (
          <div className="p-8 text-center text-xs text-muted-foreground">
            No files yet. Upload some data above.
          </div>
        )}
      </div>
    </>
  );
}
