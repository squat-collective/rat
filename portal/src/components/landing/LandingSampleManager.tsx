"use client";

import { useCallback, useState } from "react";
import { useApiClient } from "@/providers/api-provider";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { cn, formatBytes } from "@/lib/utils";
import { ChevronDown, ChevronRight, FlaskConical, Trash2 } from "lucide-react";
import { mutate } from "swr";
import { KEYS } from "@/lib/cache-keys";
import type { SampleFileListResponse } from "@squat-collective/rat-client";

interface LandingSampleManagerProps {
  ns: string;
  name: string;
  samplesData: SampleFileListResponse | undefined;
  onError: () => void;
}

export function LandingSampleManager({ ns, name, samplesData, onError }: LandingSampleManagerProps) {
  const api = useApiClient();

  const [samplesOpen, setSamplesOpen] = useState(false);
  const [sampleUploading, setSampleUploading] = useState(false);
  const [sampleDragOver, setSampleDragOver] = useState(false);

  const uploadSampleFiles = useCallback(
    async (fileList: FileList | File[]) => {
      setSampleUploading(true);
      try {
        for (const file of Array.from(fileList)) {
          await api.landing.uploadSample(ns, name, file, file.name);
        }
        mutate(KEYS.match.landingSamples(ns, name));
      } catch (e) {
        console.error("Failed to upload sample files:", e);
        onError();
      } finally {
        setSampleUploading(false);
      }
    },
    [api, ns, name, onError],
  );

  const handleSampleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setSampleDragOver(false);
      if (e.dataTransfer.files.length > 0) {
        uploadSampleFiles(e.dataTransfer.files);
      }
    },
    [uploadSampleFiles],
  );

  const handleSampleFileInput = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (e.target.files && e.target.files.length > 0) {
        uploadSampleFiles(e.target.files);
        e.target.value = "";
      }
    },
    [uploadSampleFiles],
  );

  const handleDeleteSample = async (filename: string) => {
    try {
      await api.landing.deleteSample(ns, name, filename);
      mutate(KEYS.match.landingSamples(ns, name));
    } catch (e) {
      console.error("Failed to delete sample file:", e);
      onError();
    }
  };

  return (
    <div className="border-2 border-border/50">
      <button
        type="button"
        className="w-full flex items-center gap-2 px-3 py-2.5 text-left hover:bg-muted/30 transition-colors"
        onClick={() => setSamplesOpen(!samplesOpen)}
        aria-expanded={samplesOpen}
        aria-label="Toggle preview samples section"
      >
        {samplesOpen ? (
          <ChevronDown className="h-3 w-3 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3 w-3 text-muted-foreground" />
        )}
        <FlaskConical className="h-3 w-3 text-muted-foreground" />
        <span className="text-[10px] font-bold tracking-wider">
          Preview Samples
        </span>
        <span className="text-[9px] text-muted-foreground">
          &middot; Small curated files used during pipeline preview
        </span>
        {(samplesData?.total ?? 0) > 0 && (
          <Badge variant="outline" className="text-[9px] ml-auto">
            {samplesData!.total}
          </Badge>
        )}
      </button>
      {samplesOpen && (
        <div className="border-t border-border/30 p-3 space-y-3">
          {/* Sample Drop Zone */}
          <div
            className={cn(
              "border-2 border-dashed p-4 text-center transition-all cursor-pointer",
              sampleDragOver
                ? "border-primary bg-primary/10"
                : "border-border/50 hover:border-primary/50",
            )}
            onDragOver={(e) => {
              e.preventDefault();
              setSampleDragOver(true);
            }}
            onDragLeave={() => setSampleDragOver(false)}
            onDrop={handleSampleDrop}
            onClick={() => document.getElementById("sample-upload")?.click()}
          >
            <input
              id="sample-upload"
              type="file"
              multiple
              className="hidden"
              onChange={handleSampleFileInput}
            />
            <FlaskConical className="h-5 w-5 mx-auto mb-1.5 text-muted-foreground" />
            <p className="text-[10px] text-muted-foreground">
              {sampleUploading
                ? "Uploading sample..."
                : "Drop sample files here or click to browse"}
            </p>
            <p className="text-[9px] text-muted-foreground/60 mt-0.5">
              1-3 small files with the same schema as production data
            </p>
          </div>
          {/* Sample Files Table */}
          {(samplesData?.files ?? []).length > 0 && (
            <table className="w-full text-xs">
              <thead>
                <tr>
                  <th className="whitespace-nowrap px-3 py-1.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30">
                    Filename
                  </th>
                  <th className="whitespace-nowrap px-3 py-1.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30">
                    Size
                  </th>
                  <th className="whitespace-nowrap px-3 py-1.5 text-right text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody>
                {samplesData!.files.map((sf) => {
                  const filename = sf.path.split("/").pop() ?? sf.path;
                  return (
                    <tr
                      key={sf.path}
                      className="group border-t border-border/20 hover:bg-primary/5"
                    >
                      <td className="whitespace-nowrap px-3 py-1.5 font-mono font-medium">
                        {filename}
                      </td>
                      <td className="whitespace-nowrap px-3 py-1.5 font-mono text-muted-foreground">
                        {formatBytes(sf.size)}
                      </td>
                      <td className="whitespace-nowrap px-3 py-1.5 text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-6 px-2 text-[10px] text-destructive hover:text-destructive"
                          onClick={() => handleDeleteSample(filename)}
                          aria-label={`Delete sample ${filename}`}
                        >
                          <Trash2 className="h-3 w-3" aria-hidden="true" />
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
          {(samplesData?.files ?? []).length === 0 && (
            <p className="text-center text-[10px] text-muted-foreground py-2">
              No sample files yet. Upload small, curated files for fast pipeline previews.
            </p>
          )}
        </div>
      )}
    </div>
  );
}
