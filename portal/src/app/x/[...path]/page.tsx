"use client";

import { useParams } from "next/navigation";
import { usePluginRegistry } from "@/components/plugins/plugin-context";

export default function PluginRoutePage() {
  const params = useParams<{ path: string[] }>();
  const registry = usePluginRegistry();
  const segments = params.path;

  if (!registry) {
    return <PluginRouteNotFound />;
  }

  const fullPath = `/x/${segments.join("/")}`;

  const matched = registry.routes.find((route) =>
    fullPath === route.path || fullPath.startsWith(`${route.path}/`),
  );

  if (!matched) {
    return <PluginRouteNotFound />;
  }

  const Component = matched.component;
  return <Component path={segments} />;
}

function PluginRouteNotFound() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-muted-foreground">
      <span className="text-sm tracking-wider">plugin route not found</span>
    </div>
  );
}
