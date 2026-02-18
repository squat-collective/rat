"use client";

import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useApiClient } from "@/providers/api-provider";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Plus } from "lucide-react";
import { mutate } from "swr";
import { validateName } from "@/lib/validation";
import { KEYS } from "@/lib/cache-keys";

export function CreateLandingZoneDialog() {
  const api = useApiClient();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [namespace, setNamespace] = useState("default");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const nsError = validateName(namespace);
  const nameError = validateName(name);

  const handleCreate = async () => {
    if (!name || !namespace) return;
    setLoading(true);
    setError(null);
    try {
      await api.landing.create({
        namespace,
        name,
        description: description || undefined,
      });
      setOpen(false);
      setName("");
      setDescription("");
      mutate(KEYS.match.landingZones);
      mutate(KEYS.match.lineage);
    } catch (e) {
      console.error("Failed to create landing zone:", e);
      const msg =
        e instanceof Error ? e.message : "Failed to create landing zone";
      setError(msg);
      triggerGlitch();
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <GlitchOverlay />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger asChild>
          <Button size="sm" className="gap-1">
            <Plus className="h-3 w-3" /> New Zone
          </Button>
        </DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Landing Zone</DialogTitle>
            <DialogDescription>Set up a new landing zone for raw data ingestion.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 pt-2">
            <div className="grid grid-cols-2 gap-3">
              <div>
                <Label htmlFor="create-zone-namespace" className="text-[10px]">Namespace</Label>
                <Input
                  id="create-zone-namespace"
                  value={namespace}
                  onChange={(e) => setNamespace(e.target.value)}
                  placeholder="default"
                  className="text-xs"
                />
                {nsError && (
                  <p className="text-[10px] text-destructive mt-1">{nsError}</p>
                )}
              </div>
              <div>
                <Label htmlFor="create-zone-name" className="text-[10px]">Name</Label>
                <Input
                  id="create-zone-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="raw-uploads"
                  className="text-xs"
                />
                {nameError && (
                  <p className="text-[10px] text-destructive mt-1">{nameError}</p>
                )}
              </div>
            </div>
            <div>
              <Label htmlFor="create-zone-description" className="text-[10px]">Description</Label>
              <Textarea
                id="create-zone-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What files will be dropped here?"
                className="text-xs"
                rows={2}
              />
            </div>
            {error && (
              <div className="error-block px-3 py-2 text-xs text-destructive">
                {error}
              </div>
            )}
            <Button
              onClick={handleCreate}
              disabled={loading || !name || !namespace || !!nsError || !!nameError}
              className="w-full"
            >
              {loading ? "Creating..." : "Create Zone"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
