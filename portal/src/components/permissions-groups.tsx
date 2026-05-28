"use client";

import { useState, useCallback } from "react";
import {
  useGroups,
  useCreateGroup,
  useDeleteGroup,
  useGroupMembers,
  useAddGroupMember,
  useRemoveGroupMember,
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
import {
  Plus,
  Trash2,
  Users,
  ChevronDown,
  ChevronRight,
  Loader2,
  UserPlus,
} from "lucide-react";
import type { PrincipalType } from "@squat-collective/rat-client";

export function PermissionsGroups() {
  const { data, isLoading, error } = useGroups();
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const { deleteGroup, deleting } = useDeleteGroup();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [expandedGroup, setExpandedGroup] = useState<string | null>(null);

  const handleDelete = useCallback(
    async (groupId: string) => {
      try {
        await deleteGroup(groupId);
        if (expandedGroup === groupId) setExpandedGroup(null);
      } catch {
        triggerGlitch();
      }
    },
    [deleteGroup, expandedGroup, triggerGlitch],
  );

  return (
    <div className="space-y-4">
      <GlitchOverlay />

      <div className="flex items-center justify-between">
        <p className="text-xs text-muted-foreground">
          Engine-managed groups for permission grants.
        </p>
        <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
          <DialogTrigger asChild>
            <Button size="sm" className="h-8 text-xs gap-1">
              <Plus className="h-3 w-3" /> Group
            </Button>
          </DialogTrigger>
          <CreateGroupDialog onClose={() => setCreateDialogOpen(false)} />
        </Dialog>
      </div>

      {isLoading ? (
        <Loading text="Loading groups..." />
      ) : error ? (
        <ErrorAlert error={error} prefix="Failed to load groups" />
      ) : !data?.groups?.length ? (
        <div className="brutal-card p-6 text-center">
          <p className="text-xs text-muted-foreground">
            No groups yet. Create one to get started.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {data.groups.map((group) => (
            <div key={group.group_id} className="brutal-card overflow-hidden">
              <div className="p-3 flex items-center gap-3">
                <button
                  className="text-muted-foreground hover:text-primary"
                  onClick={() =>
                    setExpandedGroup(expandedGroup === group.group_id ? null : group.group_id)
                  }
                >
                  {expandedGroup === group.group_id ? (
                    <ChevronDown className="h-4 w-4" />
                  ) : (
                    <ChevronRight className="h-4 w-4" />
                  )}
                </button>
                <Users className="h-4 w-4 text-primary shrink-0" />
                <div className="flex-1 min-w-0">
                  <p className="text-xs font-bold">{group.name}</p>
                  {group.description && (
                    <p className="text-[10px] text-muted-foreground truncate">
                      {group.description}
                    </p>
                  )}
                </div>
                <Badge variant="outline" className="text-[10px] font-mono shrink-0">
                  {group.group_id.slice(0, 8)}
                </Badge>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 w-6 p-0 text-muted-foreground hover:text-destructive shrink-0"
                  onClick={() => handleDelete(group.group_id)}
                  disabled={deleting}
                >
                  <Trash2 className="h-3 w-3" />
                </Button>
              </div>
              {expandedGroup === group.group_id && (
                <GroupMembersPanel groupId={group.group_id} />
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function GroupMembersPanel({ groupId }: { groupId: string }) {
  const { data, isLoading, error } = useGroupMembers(groupId);
  const { removeMember, removing } = useRemoveGroupMember(groupId);
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  const handleRemove = useCallback(
    async (memberType: PrincipalType, memberId: string) => {
      try {
        await removeMember(memberType, memberId);
      } catch {
        triggerGlitch();
      }
    },
    [removeMember, triggerGlitch],
  );

  return (
    <div className="border-t border-border p-3 bg-muted/10 space-y-2">
      <GlitchOverlay />
      <div className="flex items-center justify-between">
        <p className="text-[10px] tracking-wider text-muted-foreground font-bold">
          Members
        </p>
        <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
          <DialogTrigger asChild>
            <Button variant="ghost" size="sm" className="h-6 text-[10px] gap-1">
              <UserPlus className="h-3 w-3" /> Add
            </Button>
          </DialogTrigger>
          <AddMemberDialog groupId={groupId} onClose={() => setAddDialogOpen(false)} />
        </Dialog>
      </div>

      {isLoading ? (
        <Loading text="Loading members..." />
      ) : error ? (
        <ErrorAlert error={error} prefix="Failed to load members" />
      ) : !data?.members?.length ? (
        <p className="text-[10px] text-muted-foreground">No members.</p>
      ) : (
        <div className="space-y-1">
          {data.members.map((member, i) => (
            <div key={i} className="flex items-center gap-2 text-xs p-1.5">
              <Badge
                variant="outline"
                className={`text-[10px] ${member.member_type === "user" ? "bg-blue-500/20 text-blue-400" : "bg-purple-500/20 text-purple-400"}`}
              >
                {member.member_type}
              </Badge>
              <span className="font-mono flex-1">{member.member_id}</span>
              <Button
                variant="ghost"
                size="sm"
                className="h-5 w-5 p-0 text-muted-foreground hover:text-destructive"
                onClick={() => handleRemove(member.member_type as PrincipalType, member.member_id)}
                disabled={removing}
              >
                <Trash2 className="h-2.5 w-2.5" />
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CreateGroupDialog({ onClose }: { onClose: () => void }) {
  const { createGroup, creating, error } = useCreateGroup();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const handleSubmit = async () => {
    try {
      await createGroup(name, description || undefined);
      onClose();
    } catch {
      triggerGlitch();
    }
  };

  return (
    <DialogContent className="sm:max-w-md">
      <GlitchOverlay />
      <DialogHeader>
        <DialogTitle className="text-sm tracking-wider">Create Group</DialogTitle>
      </DialogHeader>
      <div className="space-y-4">
        <div>
          <Label className="text-[10px] tracking-wider">Name</Label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. data-engineering"
            className="text-xs h-8"
          />
        </div>
        <div>
          <Label className="text-[10px] tracking-wider">Description</Label>
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Optional description"
            className="text-xs h-8"
          />
        </div>
        {error && <p className="text-xs text-destructive">{error.message}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={creating || !name}>
            {creating ? <Loader2 className="h-3 w-3 animate-spin mr-1" /> : null}
            Create Group
          </Button>
        </div>
      </div>
    </DialogContent>
  );
}

function AddMemberDialog({ groupId, onClose }: { groupId: string; onClose: () => void }) {
  const { addMember, adding } = useAddGroupMember(groupId);
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();
  const [memberType, setMemberType] = useState<PrincipalType>("user");
  const [memberId, setMemberId] = useState("");

  const handleSubmit = async () => {
    try {
      await addMember(memberType, memberId);
      onClose();
    } catch {
      triggerGlitch();
    }
  };

  return (
    <DialogContent className="sm:max-w-md">
      <GlitchOverlay />
      <DialogHeader>
        <DialogTitle className="text-sm tracking-wider">Add Member</DialogTitle>
      </DialogHeader>
      <div className="space-y-4">
        <div>
          <Label className="text-[10px] tracking-wider">Member Type</Label>
          <Select value={memberType} onValueChange={(v) => setMemberType(v as PrincipalType)}>
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="user">User</SelectItem>
              <SelectItem value="group">Group</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div>
          <Label className="text-[10px] tracking-wider">
            {memberType === "user" ? "User ID" : "Group ID"}
          </Label>
          <Input
            value={memberId}
            onChange={(e) => setMemberId(e.target.value)}
            placeholder={memberType === "user" ? "e.g. bob" : "e.g. group-id"}
            className="text-xs h-8 font-mono"
          />
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={adding || !memberId}>
            {adding ? <Loader2 className="h-3 w-3 animate-spin mr-1" /> : null}
            Add Member
          </Button>
        </div>
      </div>
    </DialogContent>
  );
}
