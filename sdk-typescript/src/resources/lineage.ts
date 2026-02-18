import type { LineageGraph } from "../models/lineage";
import { BaseResource } from "./base";

export class LineageResource extends BaseResource {
  async get(params?: { namespace?: string }): Promise<LineageGraph> {
    const qp: Record<string, string> = {};
    if (params?.namespace) qp.namespace = params.namespace;
    return this.transport.request<LineageGraph>(
      "GET",
      "/api/v1/lineage",
      Object.keys(qp).length > 0 ? { params: qp } : undefined,
    );
  }
}
