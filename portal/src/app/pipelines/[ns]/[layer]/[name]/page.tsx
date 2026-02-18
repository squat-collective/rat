"use client";

import { useParams, useSearchParams } from "next/navigation";
import { useState, useCallback, useEffect, Suspense } from "react";
import { usePipeline } from "@/hooks/use-api";
import { useApiClient } from "@/providers/api-provider";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PipelineQuality } from "@/components/pipeline-quality";
import {
  PipelineHeader,
  PipelineOverview,
  PipelineEditor,
  PipelineSettings,
} from "@/components/pipeline";
import type { PipelineVersion } from "@squat-collective/rat-client";

export default function PipelineDetailPage() {
  return (
    <Suspense fallback={<Loading text="Loading pipeline..." />}>
      <PipelineDetailInner />
    </Suspense>
  );
}

function PipelineDetailInner() {
  const params = useParams<{ ns: string; layer: string; name: string }>();
  const searchParams = useSearchParams();
  const tabParam = searchParams.get("tab");
  const initialTab = tabParam === "code" ? "code" : tabParam === "settings" ? "settings" : tabParam === "quality" ? "quality" : "overview";
  const api = useApiClient();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  const { data: pipeline, isLoading, error: pipelineError } = usePipeline(
    params.ns,
    params.layer,
    params.name,
  );

  // Version history — shared between header (badge) and overview tab
  const [versions, setVersions] = useState<PipelineVersion[]>([]);
  const [versionsLoading, setVersionsLoading] = useState(false);

  const fetchVersions = useCallback(async () => {
    setVersionsLoading(true);
    try {
      const res = await api.pipelines.listVersions(params.ns, params.layer, params.name);
      setVersions(res.versions ?? []);
    } catch {
      // Versions endpoint may not exist yet (pre-migration) — ignore
    } finally {
      setVersionsLoading(false);
    }
  }, [api, params.ns, params.layer, params.name]);

  useEffect(() => {
    fetchVersions();
  }, [fetchVersions]);

  if (isLoading) return <Loading text="Loading pipeline..." />;
  if (pipelineError) return <ErrorAlert error={pipelineError} prefix="Failed to load pipeline" />;
  if (!pipeline) {
    return (
      <div className="error-block p-4 text-xs">Pipeline not found</div>
    );
  }

  return (
    <div className="space-y-4">
      <GlitchOverlay />

      {/* Header */}
      <PipelineHeader
        pipeline={pipeline}
        versions={versions}
        triggerGlitch={triggerGlitch}
      />

      {/* Tabs */}
      <Tabs defaultValue={initialTab}>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="code">Code</TabsTrigger>
          <TabsTrigger value="quality">Quality</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        {/* Overview Tab */}
        <TabsContent value="overview" className="space-y-4">
          <PipelineOverview
            pipeline={pipeline}
            versions={versions}
            versionsLoading={versionsLoading}
            onVersionsRefresh={fetchVersions}
            triggerGlitch={triggerGlitch}
          />
        </TabsContent>

        {/* Code Tab */}
        <TabsContent value="code" className="mt-0">
          <PipelineEditor
            pipeline={pipeline}
            onVersionsRefresh={fetchVersions}
            triggerGlitch={triggerGlitch}
          />
        </TabsContent>

        {/* Quality Tab */}
        <TabsContent value="quality" className="space-y-4">
          <PipelineQuality
            ns={params.ns}
            layer={params.layer}
            name={params.name}
          />
        </TabsContent>

        {/* Settings Tab */}
        <TabsContent value="settings" className="space-y-4">
          <PipelineSettings
            pipeline={pipeline}
            triggerGlitch={triggerGlitch}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}
