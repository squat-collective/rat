import type {
  CreateTriggerRequest,
  PipelineTrigger,
  TriggerListResponse,
  UpdateTriggerRequest,
} from "../models/triggers";
import { BaseResource } from "./base";

export class TriggersResource extends BaseResource {
  async list(
    ns: string,
    layer: string,
    name: string,
  ): Promise<TriggerListResponse> {
    return this.transport.request<TriggerListResponse>(
      "GET",
      `/api/v1/pipelines/${ns}/${layer}/${name}/triggers`,
    );
  }

  async get(
    ns: string,
    layer: string,
    name: string,
    triggerId: string,
  ): Promise<PipelineTrigger> {
    return this.transport.request<PipelineTrigger>(
      "GET",
      `/api/v1/pipelines/${ns}/${layer}/${name}/triggers/${triggerId}`,
    );
  }

  async create(
    ns: string,
    layer: string,
    name: string,
    req: CreateTriggerRequest,
  ): Promise<PipelineTrigger> {
    return this.transport.request<PipelineTrigger>(
      "POST",
      `/api/v1/pipelines/${ns}/${layer}/${name}/triggers`,
      { json: req },
    );
  }

  async update(
    ns: string,
    layer: string,
    name: string,
    triggerId: string,
    req: UpdateTriggerRequest,
  ): Promise<PipelineTrigger> {
    return this.transport.request<PipelineTrigger>(
      "PUT",
      `/api/v1/pipelines/${ns}/${layer}/${name}/triggers/${triggerId}`,
      { json: req },
    );
  }

  async delete(
    ns: string,
    layer: string,
    name: string,
    triggerId: string,
  ): Promise<void> {
    await this.transport.request(
      "DELETE",
      `/api/v1/pipelines/${ns}/${layer}/${name}/triggers/${triggerId}`,
    );
  }
}
