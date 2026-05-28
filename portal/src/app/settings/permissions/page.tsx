"use client";

import { useFeatures } from "@/hooks/use-api";
import { Loading } from "@/components/loading";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { PermissionsClient } from "@/components/permissions-client";

export default function PermissionsPage() {
  const { data: features, isLoading } = useFeatures();

  if (isLoading) return <Loading text="Loading permissions..." />;

  const permissionEnabled = features?.plugins?.permission?.enabled;

  if (!permissionEnabled) {
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
            Permissions
          </h1>
        </div>
        <div className="brutal-card p-6 text-center space-y-2">
          <p className="text-sm text-muted-foreground">
            Permission management requires the ACL plugin.
          </p>
          <p className="text-xs text-muted-foreground">
            Enable the <code className="text-primary font-mono">permission</code> plugin in your
            Pro configuration to manage grants, groups, and verbs.
          </p>
        </div>
      </div>
    );
  }

  return <PermissionsClient />;
}
