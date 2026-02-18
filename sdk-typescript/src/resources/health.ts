import type { FeaturesResponse, HealthResponse } from "../models/health";
import { BaseResource } from "./base";

export class HealthResource extends BaseResource {
  async getHealth(): Promise<HealthResponse> {
    return this.transport.request<HealthResponse>("GET", "/health");
  }

  async getFeatures(): Promise<FeaturesResponse> {
    return this.transport.request<FeaturesResponse>("GET", "/api/v1/features");
  }
}
