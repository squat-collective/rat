"use client";

import { useState } from "react";
import { ChevronRight, ChevronDown, Table2, Columns3 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { SchemaData } from "@/lib/sql-schema";

type SchemaTreeProps = {
  schema: SchemaData;
  onInsertTable: (text: string) => void;
};

const LAYER_ICONS: Record<string, string> = {
  bronze: "\u{1F949}",
  silver: "\u{1F948}",
  gold: "\u{1F947}",
};

const LAYER_COLORS: Record<string, string> = {
  bronze: "border-orange-700",
  silver: "border-zinc-400",
  gold: "border-yellow-500",
};

function TreeToggle({ expanded }: { expanded: boolean }) {
  return expanded ? (
    <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" />
  ) : (
    <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
  );
}

export function SchemaTree({ schema, onInsertTable }: SchemaTreeProps) {
  const [expandedNs, setExpandedNs] = useState<Set<string>>(
    () => new Set(Object.keys(schema)),
  );
  const [expandedLayers, setExpandedLayers] = useState<Set<string>>(new Set());
  const [expandedTables, setExpandedTables] = useState<Set<string>>(new Set());

  const toggleSet = (
    set: Set<string>,
    key: string,
    setter: (s: Set<string>) => void,
  ) => {
    const next = new Set(set);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setter(next);
  };

  const namespaces = Object.keys(schema);

  if (namespaces.length === 0) {
    return (
      <div className="p-3 text-[11px] text-muted-foreground">
        No tables found
      </div>
    );
  }

  return (
    <div className="text-[11px] font-mono overflow-y-auto">
      {namespaces.map((ns) => (
        <div key={ns}>
          <button
            onClick={() => toggleSet(expandedNs, ns, setExpandedNs)}
            className="flex items-center gap-1 w-full px-2 py-1 hover:bg-accent/50 text-left font-semibold tracking-wider text-primary"
          >
            <TreeToggle expanded={expandedNs.has(ns)} />
            {ns}
          </button>

          {expandedNs.has(ns) &&
            Object.keys(schema[ns]).map((layer) => {
              const layerKey = `${ns}.${layer}`;
              return (
                <div
                  key={layerKey}
                  className={cn(
                    "ml-2 border-l-2",
                    LAYER_COLORS[layer] || "border-border",
                  )}
                >
                  <button
                    onClick={() =>
                      toggleSet(expandedLayers, layerKey, setExpandedLayers)
                    }
                    className="flex items-center gap-1 w-full px-2 py-0.5 hover:bg-accent/50 text-left"
                  >
                    <TreeToggle expanded={expandedLayers.has(layerKey)} />
                    <span>
                      {LAYER_ICONS[layer] || ""} {layer}
                    </span>
                  </button>

                  {expandedLayers.has(layerKey) &&
                    Object.keys(schema[ns][layer]).map((table) => {
                      const tableKey = `${ns}.${layer}.${table}`;
                      const columns = schema[ns][layer][table];
                      return (
                        <div key={tableKey} className="ml-3">
                          <div className="flex items-center gap-1">
                            <button
                              onClick={() =>
                                toggleSet(
                                  expandedTables,
                                  tableKey,
                                  setExpandedTables,
                                )
                              }
                              className="flex items-center gap-1 py-0.5 hover:bg-accent/50"
                            >
                              <TreeToggle
                                expanded={expandedTables.has(tableKey)}
                              />
                              <Table2 className="h-3 w-3 shrink-0 text-muted-foreground" />
                            </button>
                            <button
                              onClick={() => onInsertTable(tableKey)}
                              className="hover:text-primary hover:underline truncate text-left"
                              title={`Insert ${tableKey}`}
                            >
                              {table}
                            </button>
                          </div>

                          {expandedTables.has(tableKey) && (
                            <div className="ml-5">
                              {Object.entries(columns).map(
                                ([col, colType]) => (
                                  <div
                                    key={col}
                                    className="flex items-center gap-1.5 py-0.5 text-muted-foreground"
                                  >
                                    <Columns3 className="h-2.5 w-2.5 shrink-0" />
                                    <span className="truncate">{col}</span>
                                    <span className="text-[9px] opacity-50 shrink-0">
                                      {colType}
                                    </span>
                                  </div>
                                ),
                              )}
                            </div>
                          )}
                        </div>
                      );
                    })}
                </div>
              );
            })}
        </div>
      ))}
    </div>
  );
}
