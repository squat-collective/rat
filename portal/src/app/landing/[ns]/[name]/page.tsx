"use client";

import { useParams, useRouter } from "next/navigation";
import { useCallback } from "react";
import { useApiClient } from "@/providers/api-provider";
import { useLandingZone, useLandingFiles, useLandingSamples, useProcessedFiles } from "@/hooks/use-api";
import { useScreenGlitch } from "@/components/screen-glitch";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { mutate } from "swr";
import { KEYS } from "@/lib/cache-keys";
import {
  LandingHeader,
  LandingMetadataEditor,
  LandingTriggerConfig,
  LandingFileManager,
  LandingSampleManager,
  LandingProcessedFiles,
} from "@/components/landing";

export default function LandingZoneDetailPage() {
  const params = useParams<{ ns: string; name: string }>();
  const router = useRouter();
  const api = useApiClient();
  const { triggerGlitch, GlitchOverlay } = useScreenGlitch();

  const { data: zone, isLoading: zoneLoading, error: zoneError } = useLandingZone(
    params.ns,
    params.name,
  );
  const { data: filesData, isLoading: filesLoading, error: filesError } = useLandingFiles(
    params.ns,
    params.name,
  );
  const { data: samplesData } = useLandingSamples(params.ns, params.name);
  const { data: processedData } = useProcessedFiles(params.ns, params.name);

  const handleDeleteZone = useCallback(async () => {
    if (!confirm(`Delete zone "${params.name}" and all its files?`)) return;
    try {
      await api.landing.delete(params.ns, params.name);
      mutate(KEYS.match.lineage);
      router.push("/landing");
    } catch (e) {
      console.error("Failed to delete landing zone:", e);
      triggerGlitch();
    }
  }, [api, params.ns, params.name, router, triggerGlitch]);

  if (zoneLoading || filesLoading)
    return <Loading text="Loading zone..." />;
  if (zoneError) return <ErrorAlert error={zoneError} prefix="Failed to load landing zone" />;
  if (filesError) return <ErrorAlert error={filesError} prefix="Failed to load zone files" />;

  if (!zone) {
    return (
      <div className="p-8 text-center text-xs text-muted-foreground">
        Landing zone not found
      </div>
    );
  }

  const files = filesData?.files ?? [];

  return (
    <>
      <GlitchOverlay />
      <div className="space-y-4">
        <LandingHeader
          zone={zone}
          samplesData={samplesData}
          onDelete={handleDeleteZone}
        />

        <LandingMetadataEditor
          ns={params.ns}
          name={params.name}
          zone={zone}
          onError={triggerGlitch}
        />

        <LandingTriggerConfig
          ns={params.ns}
          name={params.name}
          onError={triggerGlitch}
        />

        <LandingFileManager
          ns={params.ns}
          name={params.name}
          files={files}
          onError={triggerGlitch}
        />

        <LandingSampleManager
          ns={params.ns}
          name={params.name}
          samplesData={samplesData}
          onError={triggerGlitch}
        />

        <LandingProcessedFiles
          ns={params.ns}
          processedData={processedData}
          onError={triggerGlitch}
        />
      </div>
    </>
  );
}
