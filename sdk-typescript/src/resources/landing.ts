import type {
  CreateLandingZoneRequest,
  UpdateLandingZoneRequest,
  LandingFile,
  LandingFileListResponse,
  LandingZone,
  LandingZoneListResponse,
  SampleFileListResponse,
  SampleFileUploadResponse,
} from "../models/landing";
import { BaseResource } from "./base";

export class LandingResource extends BaseResource {
  async list(params?: {
    namespace?: string;
  }): Promise<LandingZoneListResponse> {
    const qp: Record<string, string> = {};
    if (params?.namespace) qp.namespace = params.namespace;
    return this.transport.request<LandingZoneListResponse>(
      "GET",
      "/api/v1/landing-zones",
      Object.keys(qp).length > 0 ? { params: qp } : undefined,
    );
  }

  async get(ns: string, name: string): Promise<LandingZone> {
    return this.transport.request<LandingZone>(
      "GET",
      `/api/v1/landing-zones/${ns}/${name}`,
    );
  }

  async create(req: CreateLandingZoneRequest): Promise<LandingZone> {
    return this.transport.request<LandingZone>(
      "POST",
      "/api/v1/landing-zones",
      { json: req },
    );
  }

  async update(ns: string, name: string, req: UpdateLandingZoneRequest): Promise<LandingZone> {
    return this.transport.request<LandingZone>(
      "PUT",
      `/api/v1/landing-zones/${ns}/${name}`,
      { json: req },
    );
  }

  async delete(ns: string, name: string): Promise<void> {
    await this.transport.request(
      "DELETE",
      `/api/v1/landing-zones/${ns}/${name}`,
    );
  }

  async listFiles(
    ns: string,
    name: string,
  ): Promise<LandingFileListResponse> {
    return this.transport.request<LandingFileListResponse>(
      "GET",
      `/api/v1/landing-zones/${ns}/${name}/files`,
    );
  }

  async uploadFile(
    ns: string,
    name: string,
    file: File | Blob,
    filename?: string,
  ): Promise<LandingFile> {
    const formData = new FormData();
    formData.append("file", file, filename);
    return this.transport.request<LandingFile>(
      "POST",
      `/api/v1/landing-zones/${ns}/${name}/files`,
      { body: formData },
    );
  }

  async getFile(
    ns: string,
    name: string,
    fileId: string,
  ): Promise<LandingFile> {
    return this.transport.request<LandingFile>(
      "GET",
      `/api/v1/landing-zones/${ns}/${name}/files/${fileId}`,
    );
  }

  async deleteFile(
    ns: string,
    name: string,
    fileId: string,
  ): Promise<void> {
    await this.transport.request(
      "DELETE",
      `/api/v1/landing-zones/${ns}/${name}/files/${fileId}`,
    );
  }

  async listSamples(
    ns: string,
    name: string,
  ): Promise<SampleFileListResponse> {
    return this.transport.request<SampleFileListResponse>(
      "GET",
      `/api/v1/landing-zones/${ns}/${name}/samples`,
    );
  }

  async uploadSample(
    ns: string,
    name: string,
    file: File | Blob,
    filename?: string,
  ): Promise<SampleFileUploadResponse> {
    const formData = new FormData();
    formData.append("file", file, filename);
    return this.transport.request<SampleFileUploadResponse>(
      "POST",
      `/api/v1/landing-zones/${ns}/${name}/samples`,
      { body: formData },
    );
  }

  async deleteSample(
    ns: string,
    name: string,
    filename: string,
  ): Promise<void> {
    await this.transport.request(
      "DELETE",
      `/api/v1/landing-zones/${ns}/${name}/samples/${encodeURIComponent(filename)}`,
    );
  }
}
