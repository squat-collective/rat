"use client";

import { useFeatures } from "@/hooks/use-api";
import { Loading } from "@/components/loading";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { UserListClient } from "./user-list-client";

export default function AdminUsersPage() {
  const { data: features, isLoading } = useFeatures();

  if (isLoading) return <Loading text="Loading users..." />;

  const identityEnabled = features?.plugins?.identity?.enabled;

  if (!identityEnabled) {
    return (
      <div className="space-y-6 max-w-3xl">
        <div>
          <Link
            href="/settings"
            className="text-[10px] text-muted-foreground hover:text-primary flex items-center gap-1 mb-2"
          >
            <ArrowLeft className="h-3 w-3" /> Back to settings
          </Link>
          <h1 className="text-lg font-bold tracking-wider gradient-text">
            User Management
          </h1>
        </div>
        <div className="brutal-card p-6 text-center space-y-2">
          <p className="text-sm text-muted-foreground">
            User management requires the identity plugin.
          </p>
          <p className="text-xs text-muted-foreground">
            Enable the <code className="text-primary font-mono">identity</code> plugin in your
            Pro configuration to manage users and groups from your identity provider.
          </p>
        </div>
      </div>
    );
  }

  return <UserListClient />;
}
