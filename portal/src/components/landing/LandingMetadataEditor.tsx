"use client";

import { useCallback, useReducer, useState } from "react";
import { useUpdateLandingZone } from "@/hooks/use-api";
import { Button } from "@/components/ui/button";
import { ChevronDown, ChevronRight, FileText, Save } from "lucide-react";
import type { LandingZone } from "@squat-collective/rat-client";

interface LandingMetadataEditorProps {
  ns: string;
  name: string;
  zone: LandingZone;
  onError: () => void;
}

interface MetaFormState {
  description: string | null;
  owner: string | null;
  expected_schema: string | null;
}

type MetaFormAction =
  | { type: "SET_FIELD"; field: keyof MetaFormState; value: string }
  | { type: "RESET" };

function metaFormReducer(state: MetaFormState, action: MetaFormAction): MetaFormState {
  switch (action.type) {
    case "SET_FIELD":
      return { ...state, [action.field]: action.value };
    case "RESET":
      return { description: null, owner: null, expected_schema: null };
    default:
      return state;
  }
}

export function LandingMetadataEditor({ ns, name, zone, onError }: LandingMetadataEditorProps) {
  const { update: updateZone, updating: zoneUpdating } = useUpdateLandingZone(ns, name);

  const [metaOpen, setMetaOpen] = useState(false);
  const [form, dispatch] = useReducer(metaFormReducer, {
    description: null,
    owner: null,
    expected_schema: null,
  });

  /** Whether the form has unsaved changes relative to the saved zone state. */
  const isDirty =
    (form.description !== null && form.description !== (zone?.description ?? "")) ||
    (form.owner !== null && form.owner !== (zone?.owner ?? "")) ||
    (form.expected_schema !== null && form.expected_schema !== (zone?.expected_schema ?? ""));

  const handleSaveMeta = useCallback(async () => {
    try {
      await updateZone({
        description: form.description ?? zone?.description,
        owner: form.owner ?? zone?.owner,
        expected_schema: form.expected_schema ?? zone?.expected_schema,
      });
      dispatch({ type: "RESET" });
    } catch (e) {
      console.error("Failed to save landing zone metadata:", e);
      onError();
    }
  }, [updateZone, form, zone, onError]);

  return (
    <div className="border-2 border-border/50">
      <button
        type="button"
        className="w-full flex items-center gap-2 px-3 py-2.5 text-left hover:bg-muted/30 transition-colors"
        onClick={() => setMetaOpen(!metaOpen)}
      >
        {metaOpen ? (
          <ChevronDown className="h-3 w-3 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3 w-3 text-muted-foreground" />
        )}
        <FileText className="h-3 w-3 text-muted-foreground" />
        <span className="text-[10px] font-bold tracking-wider">
          Documentation
        </span>
        <span className="text-[9px] text-muted-foreground">
          &middot; Description, owner, expected schema
        </span>
      </button>
      {metaOpen && (
        <div className="border-t border-border/30 p-3 space-y-3">
          <div className="space-y-1">
            <label htmlFor="landing-zone-description" className="text-[10px] font-bold tracking-wider text-muted-foreground">
              Description
            </label>
            <textarea
              id="landing-zone-description"
              className="w-full bg-background border border-border/50 p-2 text-xs font-mono resize-y min-h-[60px]"
              value={form.description ?? zone?.description ?? ""}
              onChange={(e) => dispatch({ type: "SET_FIELD", field: "description", value: e.target.value })}
              placeholder="Describe this landing zone..."
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="landing-zone-owner" className="text-[10px] font-bold tracking-wider text-muted-foreground">
              Owner
            </label>
            <input
              id="landing-zone-owner"
              type="text"
              className="w-full bg-background border border-border/50 p-2 text-xs font-mono"
              value={form.owner ?? zone?.owner ?? ""}
              onChange={(e) => dispatch({ type: "SET_FIELD", field: "owner", value: e.target.value })}
              placeholder="team-name or user@example.com"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="landing-zone-expected-schema" className="text-[10px] font-bold tracking-wider text-muted-foreground">
              Expected Schema
            </label>
            <textarea
              id="landing-zone-expected-schema"
              className="w-full bg-background border border-border/50 p-2 text-xs font-mono resize-y min-h-[80px]"
              value={form.expected_schema ?? zone?.expected_schema ?? ""}
              onChange={(e) => dispatch({ type: "SET_FIELD", field: "expected_schema", value: e.target.value })}
              placeholder="Document the expected file schema (columns, types, format)..."
            />
          </div>
          <Button
            size="sm"
            className="gap-1"
            onClick={handleSaveMeta}
            disabled={zoneUpdating || !isDirty}
          >
            <Save className="h-3 w-3" />
            {zoneUpdating ? "Saving..." : "Save"}
          </Button>
          {isDirty && (
            <span className="text-[9px] text-yellow-500 ml-2">Unsaved changes</span>
          )}
        </div>
      )}
    </div>
  );
}
