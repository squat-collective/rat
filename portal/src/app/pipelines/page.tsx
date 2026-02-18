import type { Metadata } from "next";
import { serverApi, type PipelineListResponse } from "@/lib/server-api";
import { PipelinesClient } from "./pipelines-client";

export const metadata: Metadata = {
  title: "Pipelines | RAT",
  description: "Manage and monitor your data pipelines",
};

export default async function PipelinesPage() {
  let data: PipelineListResponse = { pipelines: [], total: 0 };
  try {
    data = await serverApi.pipelines.list();
  } catch {
    // API unreachable
  }

  return <PipelinesClient pipelines={data.pipelines ?? []} total={data.total} />;
}
