"use client";

import { useParams } from "next/navigation";
import { useCallback, useState } from "react";
import { useTable, useTablePreview, useUpdateTableMetadata } from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { DataTable } from "@/components/data-table";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import Link from "next/link";
import { ArrowLeft, Save } from "lucide-react";

const TABS = ["Schema", "Docs", "Preview"] as const;
type Tab = (typeof TABS)[number];

export default function TableDetailPage() {
  const { ns, layer, name } = useParams<{
    ns: string;
    layer: string;
    name: string;
  }>();
  const [activeTab, setActiveTab] = useState<Tab>("Schema");

  return (
    <div className="space-y-4">
      <div>
        <Link
          href="/explorer"
          className="text-[10px] text-muted-foreground hover:text-primary flex items-center gap-1 mb-2"
        >
          <ArrowLeft className="h-3 w-3" /> Back to explorer
        </Link>
        <h1 className="text-lg font-bold tracking-wider">
          <span className="text-primary">{"//"}</span> {name}
        </h1>
        <div className="flex gap-2 mt-1">
          <Badge variant="secondary">{ns}</Badge>
          <Badge variant="outline">{layer}</Badge>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b">
        {TABS.map((tab) => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            className={cn(
              "px-3 py-2 text-[10px] font-bold tracking-wider border-b-2 transition-all -mb-px",
              activeTab === tab
                ? "border-primary text-primary neon-text"
                : "border-transparent text-muted-foreground hover:text-foreground hover:border-border",
            )}
          >
            {tab}
          </button>
        ))}
      </div>

      {activeTab === "Schema" && (
        <SchemaTab ns={ns} layer={layer} name={name} />
      )}
      {activeTab === "Docs" && (
        <DocsTab ns={ns} layer={layer} name={name} />
      )}
      {activeTab === "Preview" && (
        <PreviewTab ns={ns} layer={layer} name={name} />
      )}
    </div>
  );
}

function SchemaTab({
  ns,
  layer,
  name,
}: {
  ns: string;
  layer: string;
  name: string;
}) {
  const { data, isLoading, error } = useTable(ns, layer, name);
  if (isLoading) return <Loading text="Loading schema..." />;
  if (error) return <ErrorAlert error={error} prefix="Failed to load schema" />;

  const columns = data?.columns ?? [];
  if (!columns.length)
    return <p className="text-muted-foreground">No schema data</p>;

  return (
    <DataTable
      columns={["Name", "Type", "Description"]}
      rows={columns.map((col) => ({
        Name: col.name,
        Type: col.type,
        Description: col.description ?? "",
      }))}
    />
  );
}

function DocsTab({
  ns,
  layer,
  name,
}: {
  ns: string;
  layer: string;
  name: string;
}) {
  const { data, isLoading, error } = useTable(ns, layer, name);
  const { update, updating } = useUpdateTableMetadata(ns, layer, name);
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [description, setDescription] = useState<string | null>(null);
  const [owner, setOwner] = useState<string | null>(null);
  const [colDescs, setColDescs] = useState<Record<string, string> | null>(null);

  // Initialize form state from API data
  const tableDesc = description ?? data?.description ?? "";
  const tableOwner = owner ?? data?.owner ?? "";
  const columns = data?.columns ?? [];
  const columnDescriptions = colDescs ??
    Object.fromEntries(columns.map((c) => [c.name, c.description ?? ""]));

  const handleSave = useCallback(async () => {
    try {
      await update({
        description: tableDesc,
        owner: tableOwner || null,
        column_descriptions: columnDescriptions,
      });
    } catch (e) {
      console.error("Failed to save table metadata:", e);
      triggerGlitch();
    }
  }, [update, tableDesc, tableOwner, columnDescriptions, triggerGlitch]);

  if (isLoading) return <Loading text="Loading..." />;
  if (error) return <ErrorAlert error={error} prefix="Failed to load table" />;

  return (
    <div className="space-y-4">
      <GlitchOverlay />

      {/* Table description */}
      <div className="space-y-1">
        <label htmlFor="table-description" className="text-[10px] font-bold tracking-wider text-muted-foreground">
          Table Description
        </label>
        <textarea
          id="table-description"
          className="w-full bg-background border border-border/50 p-2 text-xs font-mono resize-y min-h-[60px]"
          value={tableDesc}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Describe this table..."
        />
      </div>

      {/* Owner */}
      <div className="space-y-1">
        <label htmlFor="table-owner" className="text-[10px] font-bold tracking-wider text-muted-foreground">
          Owner
        </label>
        <input
          id="table-owner"
          type="text"
          className="w-full bg-background border border-border/50 p-2 text-xs font-mono"
          value={tableOwner}
          onChange={(e) => setOwner(e.target.value)}
          placeholder="team-name or user@example.com"
        />
      </div>

      {/* Column descriptions */}
      {columns.length > 0 && (
        <div className="space-y-1">
          <label id="table-column-descriptions-label" className="text-[10px] font-bold tracking-wider text-muted-foreground">
            Column Descriptions
          </label>
          <div className="border border-border/50 overflow-auto" aria-labelledby="table-column-descriptions-label">
            <table className="w-full text-xs">
              <thead>
                <tr>
                  <th className="px-3 py-2 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30 w-1/4">
                    Column
                  </th>
                  <th className="px-3 py-2 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30 w-1/6">
                    Type
                  </th>
                  <th className="px-3 py-2 text-left text-[10px] font-bold tracking-wider text-muted-foreground border-b border-border/30">
                    Description
                  </th>
                </tr>
              </thead>
              <tbody>
                {columns.map((col, i) => (
                  <tr
                    key={col.name}
                    className={cn(
                      "border-t border-border/20",
                      i % 2 === 0 ? "bg-transparent" : "bg-muted/30",
                    )}
                  >
                    <td className="px-3 py-1.5 font-mono font-medium">
                      {col.name}
                    </td>
                    <td className="px-3 py-1.5 font-mono text-muted-foreground">
                      {col.type}
                    </td>
                    <td className="px-3 py-1.5">
                      <input
                        type="text"
                        className="w-full bg-transparent border-b border-border/30 text-xs font-mono focus:border-primary focus:outline-none py-0.5"
                        value={columnDescriptions[col.name] ?? ""}
                        onChange={(e) =>
                          setColDescs({
                            ...columnDescriptions,
                            [col.name]: e.target.value,
                          })
                        }
                        placeholder="Describe this column..."
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Save button */}
      <Button
        size="sm"
        className="gap-1"
        onClick={handleSave}
        disabled={updating}
      >
        <Save className="h-3 w-3" />
        {updating ? "Saving..." : "Save"}
      </Button>
    </div>
  );
}

function PreviewTab({
  ns,
  layer,
  name,
}: {
  ns: string;
  layer: string;
  name: string;
}) {
  const { data, isLoading, error } = useTablePreview(ns, layer, name);
  if (isLoading) return <Loading text="Loading preview..." />;
  if (error) return <ErrorAlert error={error} prefix="Failed to load preview" />;
  if (!data)
    return <p className="text-muted-foreground">No preview available</p>;

  return (
    <DataTable
      columns={data.columns.map((c) => c.name)}
      rows={data.rows}
    />
  );
}
