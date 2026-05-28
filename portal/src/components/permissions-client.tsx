"use client";

import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PermissionsGrants } from "@/components/permissions-grants";
import { PermissionsGroups } from "@/components/permissions-groups";
import { PermissionsVerbs } from "@/components/permissions-verbs";

export function PermissionsClient() {
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
        <p className="text-xs text-muted-foreground mt-1">
          Manage access grants, groups, and verb definitions.
        </p>
      </div>

      <Tabs defaultValue="grants" className="space-y-4">
        <TabsList className="grid w-full grid-cols-3">
          <TabsTrigger value="grants" className="text-xs">
            Grants
          </TabsTrigger>
          <TabsTrigger value="groups" className="text-xs">
            Groups
          </TabsTrigger>
          <TabsTrigger value="verbs" className="text-xs">
            Verbs
          </TabsTrigger>
        </TabsList>

        <TabsContent value="grants">
          <PermissionsGrants />
        </TabsContent>

        <TabsContent value="groups">
          <PermissionsGroups />
        </TabsContent>

        <TabsContent value="verbs">
          <PermissionsVerbs />
        </TabsContent>
      </Tabs>
    </div>
  );
}
