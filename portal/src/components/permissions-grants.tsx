"use client";

import { useState, useCallback } from "react";
import {
  useGrants,
  useCreateGrant,
  useRevokeGrant,
  useVerbs,
  useResourceAccess,
} from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Plus, Trash2, Search, X, Loader2, Eye } from "lucide-react";
import type { PrincipalType, CreateGrantRequest } from "@squat-collective/rat-client";

const PRINCIPAL_TYPES: PrincipalType[] = ["user", "group", "role"];

const verbColors: Record<string, string> = {
  admin: "bg-red-500/20 text-red-400 border-red-500/30",
  write: "bg-orange-500/20 text-orange-400 border-orange-500/30",
  read: "bg-green-500/20 text-green-400 border-green-500/30",
  execute: "bg-blue-500/20 text-blue-400 border-blue-500/30",
  publish: "bg-purple-500/20 text-purple-400 border-purple-500/30",
  delete: "bg-red-500/20 text-red-300 border-red-500/30",
};

function VerbBadge({ verb }: { verb: string }) {
  const color = verbColors[verb] ?? "bg-muted text-muted-foreground border-border";
  return (
    <Badge variant="outline" className={`text-[10px] ${color}`}>
      {verb}
    </Badge>
  );
}

function PrincipalBadge({ type }: { type: string }) {
  const colors: Record<string, string> = {
    user: "bg-blue-500/20 text-blue-400",
    group: "bg-purple-500/20 text-purple-400",
    role: "bg-amber-500/20 text-amber-400",
  };
  return (
    <Badge variant="outline" className={`text-[10px] ${colors[type] ?? ""}`}>
      {type}
    </Badge>
  );
}

export function PermissionsGrants() {
  const [resourceFilter, setResourceFilter] = useState("");
  const [principalTypeFilter, setPrincipalTypeFilter] = useState<PrincipalType | "">("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [introspectResource, setIntrospectResource] = useState("");
  const [showIntrospection, setShowIntrospection] = useState(false);

  const filters = {
    ...(resourceFilter ? { resource: resourceFilter } : {}),
    ...(principalTypeFilter ? { principal_type: principalTypeFilter } : {}),
  };
  const { data, isLoading, error } = useGrants(
    Object.keys(filters).length > 0 ? filters : undefined,
  );
  const { revokeGrant, revoking } = useRevokeGrant();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  const handleRevoke = useCallback(
    async (grantId: string) => {
      try {
        await revokeGrant(grantId);
      } catch {
        triggerGlitch();
      }
    },
    [revokeGrant, triggerGlitch],
  );

  const clearFilters = () => {
    setResourceFilter("");
    setPrincipalTypeFilter("");
  };

  const hasFilters = resourceFilter || principalTypeFilter;

  return (
    <div className="space-y-4">
      <GlitchOverlay />

      {/* Filters + Create */}
      <div className="flex items-end gap-2 flex-wrap">
        <div className="flex-1 min-w-[200px]">
          <Label className="text-[10px] tracking-wider">Resource path</Label>
          <Input
            value={resourceFilter}
            onChange={(e) => setResourceFilter(e.target.value)}
            placeholder="e.g. gold/pipeline/bronze/*"
            className="text-xs h-8 font-mono"
          />
        </div>
        <div className="w-32">
          <Label className="text-[10px] tracking-wider">Principal type</Label>
          <Select value={principalTypeFilter || "all"} onValueChange={(v) => setPrincipalTypeFilter(v === "all" ? "" : v as PrincipalType)}>
            <SelectTrigger className="h-8 text-xs">
              <SelectValue placeholder="All" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All</SelectItem>
              {PRINCIPAL_TYPES.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {hasFilters && (
          <Button variant="ghost" size="sm" className="h-8 text-[10px]" onClick={clearFilters}>
            <X className="h-3 w-3 mr-1" /> Clear
          </Button>
        )}
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button size="sm" className="h-8 text-xs gap-1">
              <Plus className="h-3 w-3" /> Grant
            </Button>
          </DialogTrigger>
          <CreateGrantDialog onClose={() => setDialogOpen(false)} />
        </Dialog>
      </div>

      {/* Grants table */}
      {isLoading ? (
        <Loading text="Loading grants..." />
      ) : error ? (
        <ErrorAlert error={error} prefix="Failed to load grants" />
      ) : !data?.grants?.length ? (
        <div className="brutal-card p-6 text-center">
          <p className="text-xs text-muted-foreground">
            {hasFilters ? "No grants match your filters." : "No grants yet. Create one to get started."}
          </p>
        </div>
      ) : (
        <div className="brutal-card overflow-hidden">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border bg-muted/30">
                <th className="text-left p-2 text-[10px] tracking-wider text-muted-foreground">Principal</th>
                <th className="text-left p-2 text-[10px] tracking-wider text-muted-foreground">Resource</th>
                <th className="text-left p-2 text-[10px] tracking-wider text-muted-foreground">Verb</th>
                <th className="text-left p-2 text-[10px] tracking-wider text-muted-foreground">Granted by</th>
                <th className="w-10" />
              </tr>
            </thead>
            <tbody>
              {data.grants.map((grant, i) => (
                <tr
                  key={grant.grant_id}
                  className={`border-b border-border/50 ${i % 2 === 0 ? "" : "bg-muted/10"}`}
                >
                  <td className="p-2">
                    <div className="flex items-center gap-1.5">
                      <PrincipalBadge type={grant.principal_type} />
                      <span className="font-mono">{grant.principal_id}</span>
                    </div>
                  </td>
                  <td className="p-2 font-mono text-muted-foreground">{grant.resource}</td>
                  <td className="p-2">
                    <VerbBadge verb={grant.verb} />
                  </td>
                  <td className="p-2 text-muted-foreground">{grant.granted_by}</td>
                  <td className="p-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 w-6 p-0 text-muted-foreground hover:text-destructive"
                      onClick={() => handleRevoke(grant.grant_id)}
                      disabled={revoking}
                    >
                      <Trash2 className="h-3 w-3" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Introspection panel */}
      <div className="brutal-card p-4 space-y-3">
        <div className="flex items-center gap-2">
          <Eye className="h-4 w-4 text-primary" />
          <h3 className="text-xs font-bold tracking-wider text-muted-foreground">
            Who has access to...
          </h3>
        </div>
        <div className="flex items-end gap-2">
          <div className="flex-1">
            <Input
              value={introspectResource}
              onChange={(e) => setIntrospectResource(e.target.value)}
              placeholder="Enter a resource path, e.g. gold/pipeline/bronze/orders"
              className="text-xs h-8 font-mono"
            />
          </div>
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs gap-1"
            onClick={() => setShowIntrospection(!!introspectResource)}
            disabled={!introspectResource}
          >
            <Search className="h-3 w-3" /> Inspect
          </Button>
        </div>
        {showIntrospection && introspectResource && (
          <IntrospectionResults resource={introspectResource} />
        )}
      </div>
    </div>
  );
}

function IntrospectionResults({ resource }: { resource: string }) {
  const { data, isLoading, error } = useResourceAccess(resource);

  if (isLoading) return <Loading text="Loading access..." />;
  if (error) return <ErrorAlert error={error} prefix="Failed to load access" />;
  if (!data?.access?.length) {
    return <p className="text-xs text-muted-foreground">No one has access to this resource.</p>;
  }

  return (
    <div className="space-y-1">
      {data.access.map((a, i) => (
        <div key={i} className="flex items-center gap-2 text-xs p-1.5 bg-muted/20">
          <PrincipalBadge type={a.principal_type} />
          <span className="font-mono">{a.principal_id}</span>
          <VerbBadge verb={a.verb} />
          <Badge variant="outline" className="text-[10px] ml-auto">
            {a.source}
          </Badge>
        </div>
      ))}
    </div>
  );
}

function CreateGrantDialog({ onClose }: { onClose: () => void }) {
  const { createGrant, creating, error } = useCreateGrant();
  const { data: verbData } = useVerbs();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [form, setForm] = useState<CreateGrantRequest>({
    principal_type: "user",
    principal_id: "",
    resource: "",
    verb: "",
  });

  const handleSubmit = async () => {
    try {
      await createGrant(form);
      onClose();
    } catch {
      triggerGlitch();
    }
  };

  const isValid = form.principal_id && form.resource && form.verb;

  return (
    <DialogContent className="sm:max-w-md">
      <GlitchOverlay />
      <DialogHeader>
        <DialogTitle className="text-sm tracking-wider">Create Grant</DialogTitle>
      </DialogHeader>
      <div className="space-y-4">
        <div>
          <Label className="text-[10px] tracking-wider">Principal Type</Label>
          <Select
            value={form.principal_type}
            onValueChange={(v) => setForm((f) => ({ ...f, principal_type: v as PrincipalType }))}
          >
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PRINCIPAL_TYPES.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div>
          <Label className="text-[10px] tracking-wider">Principal ID</Label>
          <Input
            value={form.principal_id}
            onChange={(e) => setForm((f) => ({ ...f, principal_id: e.target.value }))}
            placeholder="e.g. bob or grp-data-eng"
            className="text-xs h-8 font-mono"
          />
        </div>
        <div>
          <Label className="text-[10px] tracking-wider">Resource Path</Label>
          <Input
            value={form.resource}
            onChange={(e) => setForm((f) => ({ ...f, resource: e.target.value }))}
            placeholder="e.g. gold/pipeline/bronze/orders or gold/*"
            className="text-xs h-8 font-mono"
          />
        </div>
        <div>
          <Label className="text-[10px] tracking-wider">Verb</Label>
          <Select
            value={form.verb}
            onValueChange={(v) => setForm((f) => ({ ...f, verb: v }))}
          >
            <SelectTrigger className="h-8 text-xs">
              <SelectValue placeholder="Select a verb" />
            </SelectTrigger>
            <SelectContent>
              {(verbData?.verbs ?? []).map((v) => (
                <SelectItem key={v.name} value={v.name}>
                  {v.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {error && <p className="text-xs text-destructive">{error.message}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={creating || !isValid}>
            {creating ? <Loader2 className="h-3 w-3 animate-spin mr-1" /> : null}
            Create Grant
          </Button>
        </div>
      </div>
    </DialogContent>
  );
}
