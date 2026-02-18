import type {
  CreatePipelineRequest,
  CreatePipelineResponse,
  Pipeline,
  PipelineListResponse,
  PipelineVersion,
  PipelineVersionListResponse,
  PublishResponse,
  RollbackRequest,
  RollbackResponse,
  UpdatePipelineRequest,
} from "../models/pipelines";
import type { PreviewRequest, PreviewResponse } from "../models/preview";
import { BaseResource } from "./base";

export interface PipelineListParams {
  namespace?: string;
  layer?: string;
}

export class PipelinesResource extends BaseResource {
  async list(params?: PipelineListParams): Promise<PipelineListResponse> {
    const qp: Record<string, string> = {};
    if (params?.namespace) qp.namespace = params.namespace;
    if (params?.layer) qp.layer = params.layer;
    return this.transport.request<PipelineListResponse>(
      "GET",
      "/api/v1/pipelines",
      Object.keys(qp).length > 0 ? { params: qp } : undefined,
    );
  }

  async get(ns: string, layer: string, name: string): Promise<Pipeline> {
    return this.transport.request<Pipeline>(
      "GET",
      `/api/v1/pipelines/${ns}/${layer}/${name}`,
    );
  }

  async create(req: CreatePipelineRequest): Promise<CreatePipelineResponse> {
    return this.transport.request<CreatePipelineResponse>(
      "POST",
      "/api/v1/pipelines",
      { json: req },
    );
  }

  async update(
    ns: string,
    layer: string,
    name: string,
    req: UpdatePipelineRequest,
  ): Promise<Pipeline> {
    return this.transport.request<Pipeline>(
      "PUT",
      `/api/v1/pipelines/${ns}/${layer}/${name}`,
      { json: req },
    );
  }

  async delete(ns: string, layer: string, name: string): Promise<void> {
    await this.transport.request(
      "DELETE",
      `/api/v1/pipelines/${ns}/${layer}/${name}`,
    );
  }

  async preview(
    ns: string,
    layer: string,
    name: string,
    req?: PreviewRequest,
  ): Promise<PreviewResponse> {
    return this.transport.request<PreviewResponse>(
      "POST",
      `/api/v1/pipelines/${ns}/${layer}/${name}/preview`,
      req ? { json: req } : undefined,
    );
  }

  async publish(
    ns: string,
    layer: string,
    name: string,
    message?: string,
  ): Promise<PublishResponse> {
    return this.transport.request<PublishResponse>(
      "POST",
      `/api/v1/pipelines/${ns}/${layer}/${name}/publish`,
      message ? { json: { message } } : undefined,
    );
  }

  async listVersions(
    ns: string,
    layer: string,
    name: string,
  ): Promise<PipelineVersionListResponse> {
    return this.transport.request<PipelineVersionListResponse>(
      "GET",
      `/api/v1/pipelines/${ns}/${layer}/${name}/versions`,
    );
  }

  async getVersion(
    ns: string,
    layer: string,
    name: string,
    versionNumber: number,
  ): Promise<PipelineVersion> {
    return this.transport.request<PipelineVersion>(
      "GET",
      `/api/v1/pipelines/${ns}/${layer}/${name}/versions/${versionNumber}`,
    );
  }

  async rollback(
    ns: string,
    layer: string,
    name: string,
    version: number,
    message?: string,
  ): Promise<RollbackResponse> {
    const body: RollbackRequest = { version, message };
    return this.transport.request<RollbackResponse>(
      "POST",
      `/api/v1/pipelines/${ns}/${layer}/${name}/rollback`,
      { json: body },
    );
  }
}
