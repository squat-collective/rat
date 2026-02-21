"use client";

import { useState } from "react";
import {
  usePluginPolicies,
  useCreatePluginPolicy,
  useDeletePluginPolicy,
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
  DialogFooter,
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
import { Plus, Trash2, Loader2 } from "lucide-react";

export function PluginPolicies() {
  const { data: policies, error, isLoading } = usePluginPolicies();
  const { create, creating, error: createError } = useCreatePluginPolicy();
  const { deletePolicy } = useDeletePluginPolicy();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [newRule, setNewRule] = useState<"allow" | "deny">("allow");
  const [newPattern, setNewPattern] = useState("");
  const [newKind, setNewKind] = useState("");
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);

  if (isLoading) return <Loading text="Loading policies..." />;
  if (error) {
    triggerGlitch();
    return (
      <>
        <GlitchOverlay />
        <ErrorAlert error={error} prefix="Plugin Policies" />
      </>
    );
  }

  const handleCreate = async () => {
    if (!newPattern.trim()) return;
    try {
      await create({
        rule: newRule,
        pattern: newPattern.trim(),
        kind: newKind || undefined,
      });
      setDialogOpen(false);
      setNewPattern("");
      setNewKind("");
    } catch {
      triggerGlitch();
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deletePolicy(id);
    } catch {
      triggerGlitch();
    } finally {
      setDeleteConfirm(null);
    }
  };

  return (
    <>
      <GlitchOverlay />
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <p className="text-xs text-muted-foreground">
            Policies control which plugins are allowed or denied from registering.
          </p>
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger asChild>
              <Button size="sm" className="text-[10px] h-7">
                <Plus className="h-3 w-3 mr-1" />
                Add Policy
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Add Plugin Policy</DialogTitle>
              </DialogHeader>
              <div className="space-y-3">
                <div className="space-y-1">
                  <Label className="text-[10px] tracking-wider">Rule</Label>
                  <Select
                    value={newRule}
                    onValueChange={(v) => setNewRule(v as "allow" | "deny")}
                  >
                    <SelectTrigger className="text-xs h-8">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="allow" className="text-xs">
                        Allow
                      </SelectItem>
                      <SelectItem value="deny" className="text-xs">
                        Deny
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1">
                  <Label className="text-[10px] tracking-wider">Pattern</Label>
                  <Input
                    value={newPattern}
                    onChange={(e) => setNewPattern(e.target.value)}
                    placeholder="auth-*"
                    className="text-xs h-8 font-mono"
                  />
                  <p className="text-[10px] text-muted-foreground">
                    Glob pattern to match plugin names (e.g. &quot;auth-*&quot;,
                    &quot;*&quot;)
                  </p>
                </div>
                <div className="space-y-1">
                  <Label className="text-[10px] tracking-wider">
                    Kind{" "}
                    <span className="text-muted-foreground font-normal">
                      (optional)
                    </span>
                  </Label>
                  <Select value={newKind} onValueChange={setNewKind}>
                    <SelectTrigger className="text-xs h-8">
                      <SelectValue placeholder="All kinds" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="" className="text-xs">
                        All kinds
                      </SelectItem>
                      <SelectItem value="platform" className="text-xs">
                        Platform
                      </SelectItem>
                      <SelectItem value="runner" className="text-xs">
                        Runner
                      </SelectItem>
                      <SelectItem value="portal" className="text-xs">
                        Portal
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                {createError && (
                  <p className="text-[10px] text-destructive">
                    {createError.message}
                  </p>
                )}
              </div>
              <DialogFooter>
                <Button
                  variant="ghost"
                  onClick={() => setDialogOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  onClick={handleCreate}
                  disabled={creating || !newPattern.trim()}
                >
                  {creating ? (
                    <Loader2 className="h-3 w-3 animate-spin mr-1" />
                  ) : null}
                  Create
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>

        {!policies || policies.length === 0 ? (
          <div className="brutal-card p-6 text-center">
            <p className="text-xs text-muted-foreground">
              No plugin policies configured. All plugins are allowed by default.
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {policies.map((policy) => (
              <div
                key={policy.id}
                className="brutal-card p-3 flex items-center gap-2"
              >
                <Badge
                  variant="outline"
                  className={`text-[10px] px-1.5 py-0 ${
                    policy.rule === "allow"
                      ? "text-primary border-primary/30"
                      : "text-destructive border-destructive/30"
                  }`}
                >
                  {policy.rule}
                </Badge>
                <span className="text-xs font-mono flex-1">
                  {policy.pattern}
                </span>
                <Badge
                  variant="outline"
                  className="text-[10px] px-1.5 py-0"
                >
                  {policy.kind || "all"}
                </Badge>

                {deleteConfirm === policy.id ? (
                  <div className="flex items-center gap-1">
                    <Button
                      variant="destructive"
                      size="sm"
                      className="text-[10px] h-6 px-2"
                      onClick={() => handleDelete(policy.id)}
                    >
                      Confirm
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-[10px] h-6 px-2"
                      onClick={() => setDeleteConfirm(null)}
                    >
                      Cancel
                    </Button>
                  </div>
                ) : (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6 text-destructive hover:text-destructive"
                    onClick={() => setDeleteConfirm(policy.id)}
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </>
  );
}
