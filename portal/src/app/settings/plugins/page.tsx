"use client";

import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PluginList } from "@/components/plugin-list";
import { PluginSources } from "@/components/plugin-sources";
import { PluginPolicies } from "@/components/plugin-policies";

export default function PluginManagementPage() {
  return (
    <div className="space-y-6 max-w-3xl">
      <div className="flex items-center gap-3">
        <Link
          href="/settings"
          className="text-muted-foreground hover:text-primary transition-colors"
        >
          <ArrowLeft className="h-4 w-4" />
        </Link>
        <h1 className="text-lg font-bold tracking-wider gradient-text">
          Plugin Management
        </h1>
      </div>

      <Tabs defaultValue="plugins" className="space-y-4">
        <TabsList>
          <TabsTrigger value="plugins">Plugins</TabsTrigger>
          <TabsTrigger value="sources">Sources</TabsTrigger>
          <TabsTrigger value="policies">Policies</TabsTrigger>
        </TabsList>

        <TabsContent value="plugins">
          <PluginList />
        </TabsContent>

        <TabsContent value="sources">
          <PluginSources />
        </TabsContent>

        <TabsContent value="policies">
          <PluginPolicies />
        </TabsContent>
      </Tabs>
    </div>
  );
}
