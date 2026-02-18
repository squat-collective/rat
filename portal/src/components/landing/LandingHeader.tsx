"use client";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FlaskConical, Inbox, Trash2 } from "lucide-react";
import { formatBytes } from "@/lib/utils";
import type { LandingZone, SampleFileListResponse } from "@squat-collective/rat-client";

interface LandingHeaderProps {
  zone: LandingZone;
  samplesData: SampleFileListResponse | undefined;
  onDelete: () => void;
}

export function LandingHeader({ zone, samplesData, onDelete }: LandingHeaderProps) {
  return (
    <div className="flex items-center justify-between">
      <div>
        <h1 className="text-sm font-bold tracking-wider flex items-center gap-2">
          <Inbox className="h-4 w-4" />
          {zone.name}
        </h1>
        <div className="flex items-center gap-2 mt-1">
          <Badge variant="secondary" className="text-[9px]">
            {zone.namespace}
          </Badge>
          <span className="text-[10px] text-muted-foreground">
            {zone.file_count} file{zone.file_count !== 1 ? "s" : ""}{" "}
            &middot; {formatBytes(zone.total_bytes)}
          </span>
          {(samplesData?.total ?? 0) > 0 && (
            <Badge variant="outline" className="text-[9px] gap-1">
              <FlaskConical className="h-2.5 w-2.5" />
              {samplesData!.total} sample{samplesData!.total !== 1 ? "s" : ""}
            </Badge>
          )}
        </div>
        {zone.description && (
          <p className="text-[10px] text-muted-foreground mt-1">
            {zone.description}
          </p>
        )}
      </div>
      <Button
        variant="destructive"
        size="sm"
        className="gap-1"
        onClick={onDelete}
        aria-label={`Delete landing zone ${zone.name}`}
      >
        <Trash2 className="h-3 w-3" aria-hidden="true" /> Delete
      </Button>
    </div>
  );
}
