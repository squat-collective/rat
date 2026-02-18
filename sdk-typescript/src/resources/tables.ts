import type { QueryResult } from "../models/query";
import type { TableDetail, TableListResponse, SchemaResponse, UpdateTableMetadataRequest } from "../models/tables";
import { BaseResource } from "./base";

export interface TableListParams {
  namespace?: string;
  layer?: string;
}

export class TablesResource extends BaseResource {
  async list(params?: TableListParams): Promise<TableListResponse> {
    const qp: Record<string, string> = {};
    if (params?.namespace) qp.namespace = params.namespace;
    if (params?.layer) qp.layer = params.layer;
    return this.transport.request<TableListResponse>(
      "GET",
      "/api/v1/tables",
      Object.keys(qp).length > 0 ? { params: qp } : undefined,
    );
  }

  async get(ns: string, layer: string, name: string): Promise<TableDetail> {
    return this.transport.request<TableDetail>(
      "GET",
      `/api/v1/tables/${ns}/${layer}/${name}`,
    );
  }

  async preview(ns: string, layer: string, name: string): Promise<QueryResult> {
    return this.transport.request<QueryResult>(
      "GET",
      `/api/v1/tables/${ns}/${layer}/${name}/preview`,
    );
  }

  async schema(params?: TableListParams): Promise<SchemaResponse> {
    const qp: Record<string, string> = {};
    if (params?.namespace) qp.namespace = params.namespace;
    if (params?.layer) qp.layer = params.layer;
    return this.transport.request<SchemaResponse>(
      "GET",
      "/api/v1/schema",
      Object.keys(qp).length > 0 ? { params: qp } : undefined,
    );
  }

  async updateMetadata(
    ns: string,
    layer: string,
    name: string,
    req: UpdateTableMetadataRequest,
  ): Promise<TableDetail> {
    return this.transport.request<TableDetail>(
      "PUT",
      `/api/v1/tables/${ns}/${layer}/${name}/metadata`,
      { json: req },
    );
  }
}
