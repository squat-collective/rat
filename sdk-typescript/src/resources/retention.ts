import type {
  RetentionConfig,
  RetentionConfigResponse,
  ReaperStatus,
  PipelineRetentionResponse,
  ZoneLifecycleResponse,
  ZoneLifecycleRequest,
} from "../models/retention";
import { BaseResource } from "./base";

export class RetentionResource extends BaseResource {
  /** Get system retention config. */
  async getConfig(): Promise<RetentionConfigResponse> {
    return this.transport.request<RetentionConfigResponse>(
      "GET",
      "/api/v1/admin/retention/config",
    );
  }

  /** Update system retention config. */
  async updateConfig(config: RetentionConfig): Promise<RetentionConfigResponse> {
    return this.transport.request<RetentionConfigResponse>(
      "PUT",
      "/api/v1/admin/retention/config",
      { json: config },
    );
  }

  /** Get reaper last-run status. */
  async getStatus(): Promise<ReaperStatus> {
    return this.transport.request<ReaperStatus>(
      "GET",
      "/api/v1/admin/retention/status",
    );
  }

  /** Trigger a manual reaper run. */
  async triggerRun(): Promise<ReaperStatus> {
    return this.transport.request<ReaperStatus>(
      "POST",
      "/api/v1/admin/retention/run",
    );
  }

  /** Get pipeline retention config (system + overrides + effective). */
  async getPipelineRetention(
    namespace: string,
    layer: string,
    name: string,
  ): Promise<PipelineRetentionResponse> {
    return this.transport.request<PipelineRetentionResponse>(
      "GET",
      `/api/v1/pipelines/${namespace}/${layer}/${name}/retention`,
    );
  }

  /** Update per-pipeline retention overrides. */
  async updatePipelineRetention(
    namespace: string,
    layer: string,
    name: string,
    overrides: Partial<RetentionConfig>,
  ): Promise<void> {
    await this.transport.request(
      "PUT",
      `/api/v1/pipelines/${namespace}/${layer}/${name}/retention`,
      { json: overrides },
    );
  }

  /** Get landing zone lifecycle settings. */
  async getZoneLifecycle(
    namespace: string,
    name: string,
  ): Promise<ZoneLifecycleResponse> {
    return this.transport.request<ZoneLifecycleResponse>(
      "GET",
      `/api/v1/landing-zones/${namespace}/${name}/lifecycle`,
    );
  }

  /** Update landing zone lifecycle settings. */
  async updateZoneLifecycle(
    namespace: string,
    name: string,
    settings: ZoneLifecycleRequest,
  ): Promise<void> {
    await this.transport.request(
      "PUT",
      `/api/v1/landing-zones/${namespace}/${name}/lifecycle`,
      { json: settings },
    );
  }
}
