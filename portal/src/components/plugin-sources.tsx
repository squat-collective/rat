"use client";

import { useState } from "react";
import {
  usePluginSources,
  useCreatePluginSource,
  useDeletePluginSource,
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

export function PluginSources() {
  const { data: sources, error, isLoading } = usePluginSources();
  const { create, creating, error: createError } = useCreatePluginSource();
  const { deleteSource } = useDeletePluginSource();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [newType, setNewType] = useState("oci");
  const [newUrl, setNewUrl] = useState("");
  const [newTrusted, setNewTrusted] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);

  if (isLoading) return <Loading text="Loading sources..." />;
  if (error) {
    triggerGlitch();
    return (
      <>
        <GlitchOverlay />
        <ErrorAlert error={error} prefix="Plugin Sources" />
      </>
    );
  }

  const handleCreate = async () => {
    if (!newUrl.trim()) return;
    try {
      await create({ type: newType, url: newUrl.trim(), trusted: newTrusted });
      setDialogOpen(false);
      setNewUrl("");
      setNewTrusted(false);
    } catch {
      triggerGlitch();
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteSource(id);
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
            Plugin sources define where plugins can be discovered and installed from.
          </p>
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger asChild>
              <Button size="sm" className="text-[10px] h-7">
                <Plus className="h-3 w-3 mr-1" />
                Add Source
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Add Plugin Source</DialogTitle>
              </DialogHeader>
              <div className="space-y-3">
                <div className="space-y-1">
                  <Label className="text-[10px] tracking-wider">Type</Label>
                  <Select value={newType} onValueChange={setNewType}>
                    <SelectTrigger className="text-xs h-8">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="oci" className="text-xs">
                        OCI Registry
                      </SelectItem>
                      <SelectItem value="local" className="text-xs">
                        Local Path
                      </SelectItem>
                      <SelectItem value="git" className="text-xs">
                        Git Repository
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1">
                  <Label className="text-[10px] tracking-wider">URL</Label>
                  <Input
                    value={newUrl}
                    onChange={(e) => setNewUrl(e.target.value)}
                    placeholder={
                      newType === "oci"
                        ? "registry.example.com/plugins"
                        : newType === "git"
                          ? "https://github.com/org/plugins.git"
                          : "/opt/plugins"
                    }
                    className="text-xs h-8 font-mono"
                  />
                </div>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    className={`h-4 w-4 border ${newTrusted ? "bg-primary border-primary" : "border-muted-foreground"}`}
                    onClick={() => setNewTrusted(!newTrusted)}
                  />
                  <Label className="text-[10px] tracking-wider">
                    Trusted source
                  </Label>
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
                  disabled={creating || !newUrl.trim()}
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

        {!sources || sources.length === 0 ? (
          <div className="brutal-card p-6 text-center">
            <p className="text-xs text-muted-foreground">
              No plugin sources configured.
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {sources.map((src) => (
              <div
                key={src.id}
                className="brutal-card p-3 flex items-center gap-2"
              >
                <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                  {src.type}
                </Badge>
                <span className="text-xs font-mono flex-1 truncate">
                  {src.url}
                </span>
                {src.trusted && (
                  <Badge
                    variant="outline"
                    className="text-[10px] px-1.5 py-0 text-primary border-primary/30"
                  >
                    trusted
                  </Badge>
                )}
                <Badge
                  variant="outline"
                  className={`text-[10px] px-1.5 py-0 ${src.enabled ? "text-primary border-primary/30" : "text-muted-foreground border-muted-foreground/30"}`}
                >
                  {src.enabled ? "enabled" : "disabled"}
                </Badge>

                {deleteConfirm === src.id ? (
                  <div className="flex items-center gap-1">
                    <Button
                      variant="destructive"
                      size="sm"
                      className="text-[10px] h-6 px-2"
                      onClick={() => handleDelete(src.id)}
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
                    onClick={() => setDeleteConfirm(src.id)}
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
