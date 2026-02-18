import type {
  CreateQualityTestRequest,
  CreateQualityTestResponse,
  QualityRunResponse,
  QualityTestListResponse,
} from "../models/quality";
import { BaseResource } from "./base";

export class QualityResource extends BaseResource {
  async list(
    ns: string,
    layer: string,
    name: string,
  ): Promise<QualityTestListResponse> {
    return this.transport.request<QualityTestListResponse>(
      "GET",
      `/api/v1/tests/${ns}/${layer}/${name}`,
    );
  }

  async create(
    ns: string,
    layer: string,
    name: string,
    req: CreateQualityTestRequest,
  ): Promise<CreateQualityTestResponse> {
    return this.transport.request<CreateQualityTestResponse>(
      "POST",
      `/api/v1/tests/${ns}/${layer}/${name}`,
      { json: req },
    );
  }

  async delete(
    ns: string,
    layer: string,
    name: string,
    testName: string,
  ): Promise<void> {
    await this.transport.request(
      "DELETE",
      `/api/v1/tests/${ns}/${layer}/${name}/${testName}`,
    );
  }

  async run(
    ns: string,
    layer: string,
    name: string,
  ): Promise<QualityRunResponse> {
    return this.transport.request<QualityRunResponse>(
      "POST",
      `/api/v1/tests/${ns}/${layer}/${name}/run`,
    );
  }
}
