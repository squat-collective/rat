"use client";

import { useState } from "react";
import Link from "next/link";
import { useLandingZones } from "@/hooks/use-api";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { CreateLandingZoneDialog } from "@/components/create-landing-zone-dialog";
import { cn, formatBytes } from "@/lib/utils";

export default function LandingPage() {
  const { data, isLoading, error } = useLandingZones();
  const [search, setSearch] = useState("");

  if (isLoading) return <Loading text="Loading landing zones..." />;
  if (error) return <ErrorAlert error={error} prefix="Failed to load landing zones" />;

  const zones = data?.zones ?? [];
  const filtered = zones.filter(
    (z) =>
      z.name.toLowerCase().includes(search.toLowerCase()) ||
      z.namespace.toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-sm font-bold tracking-wider">
            Landing Zones
          </h1>
          <p className="text-[10px] text-muted-foreground">
            {data?.total ?? 0} zone{(data?.total ?? 0) !== 1 ? "s" : ""}
          </p>
        </div>
        <CreateLandingZoneDialog />
      </div>

      {/* Search */}
      <Input
        placeholder="Search zones..."
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        className="text-xs max-w-sm"
      />

      {/* Table */}
      <div className="overflow-auto border-2 border-border/50">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-card/95 backdrop-blur-sm z-10">
            <tr>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30 w-8">
                #
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Name
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Namespace
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Files
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Size
              </th>
              <th className="whitespace-nowrap px-3 py-2.5 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b-2 border-primary/30">
                Description
              </th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((z, i) => (
              <tr
                key={z.id}
                className={cn(
                  "group border-t border-border/20 transition-all cursor-pointer",
                  "hover:bg-primary/5 hover:border-l-2 hover:border-l-primary",
                  i % 2 === 0 ? "bg-transparent" : "bg-muted/30",
                )}
              >
                <td className="whitespace-nowrap px-3 py-2 font-mono text-[10px] text-muted-foreground/50">
                  {(i + 1).toString().padStart(2, "0")}
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <Link
                    href={`/landing/${z.namespace}/${z.name}`}
                    className="font-medium hover:text-primary"
                  >
                    {z.name}
                  </Link>
                </td>
                <td className="whitespace-nowrap px-3 py-2">
                  <Badge variant="secondary" className="text-[9px]">
                    {z.namespace}
                  </Badge>
                </td>
                <td className="whitespace-nowrap px-3 py-2 font-mono text-muted-foreground">
                  {z.file_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 font-mono text-muted-foreground">
                  {formatBytes(z.total_bytes)}
                </td>
                <td className="px-3 py-2 text-muted-foreground truncate max-w-[300px]">
                  {z.description || "-"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {filtered.length === 0 && (
          <div className="p-8 text-center text-xs text-muted-foreground">
            {search
              ? "No zones match your search"
              : "No landing zones yet. Create one to start uploading files."}
          </div>
        )}
      </div>
    </div>
  );
}
