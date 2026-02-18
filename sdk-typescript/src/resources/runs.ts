import type {
  CreateRunRequest,
  CreateRunResponse,
  Run,
  RunListResponse,
  RunLogsResponse,
} from "../models/runs";
import { BaseResource } from "./base";

export interface RunListParams {
  namespace?: string;
  layer?: string;
  pipeline?: string;
  status?: string;
}

export class RunsResource extends BaseResource {
  async list(params?: RunListParams): Promise<RunListResponse> {
    const qp: Record<string, string> = {};
    if (params?.namespace) qp.namespace = params.namespace;
    if (params?.layer) qp.layer = params.layer;
    if (params?.pipeline) qp.pipeline = params.pipeline;
    if (params?.status) qp.status = params.status;
    return this.transport.request<RunListResponse>(
      "GET",
      "/api/v1/runs",
      Object.keys(qp).length > 0 ? { params: qp } : undefined,
    );
  }

  async get(id: string): Promise<Run> {
    return this.transport.request<Run>("GET", `/api/v1/runs/${id}`);
  }

  async create(req: CreateRunRequest): Promise<CreateRunResponse> {
    return this.transport.request<CreateRunResponse>("POST", "/api/v1/runs", {
      json: req,
    });
  }

  async cancel(id: string): Promise<{ run_id: string; status: string }> {
    return this.transport.request("POST", `/api/v1/runs/${id}/cancel`);
  }

  async logs(id: string): Promise<RunLogsResponse> {
    return this.transport.request<RunLogsResponse>(
      "GET",
      `/api/v1/runs/${id}/logs`,
    );
  }
}
