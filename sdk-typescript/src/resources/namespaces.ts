import type { Namespace, NamespaceListResponse, UpdateNamespaceRequest } from "../models/namespaces";
import { BaseResource } from "./base";

export class NamespacesResource extends BaseResource {
  async list(): Promise<NamespaceListResponse> {
    return this.transport.request<NamespaceListResponse>(
      "GET",
      "/api/v1/namespaces",
    );
  }

  async create(name: string): Promise<Namespace> {
    return this.transport.request<Namespace>("POST", "/api/v1/namespaces", {
      json: { name },
    });
  }

  async update(name: string, req: UpdateNamespaceRequest): Promise<void> {
    await this.transport.request("PUT", `/api/v1/namespaces/${name}`, {
      json: req,
    });
  }

  async delete(name: string): Promise<void> {
    await this.transport.request("DELETE", `/api/v1/namespaces/${name}`);
  }
}
