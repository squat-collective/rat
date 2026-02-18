import type { FileContent, FileListResponse } from "../models/storage";
import { BaseResource } from "./base";

export class StorageResource extends BaseResource {
  async list(prefix?: string, exclude?: string): Promise<FileListResponse> {
    const params: Record<string, string> = {};
    if (prefix) params.prefix = prefix;
    if (exclude) params.exclude = exclude;
    return this.transport.request<FileListResponse>(
      "GET",
      "/api/v1/files",
      Object.keys(params).length > 0 ? { params } : undefined,
    );
  }

  async read(path: string): Promise<FileContent> {
    return this.transport.request<FileContent>(
      "GET",
      `/api/v1/files/${path}`,
    );
  }

  async write(
    path: string,
    content: string,
  ): Promise<{ path: string; status: string }> {
    return this.transport.request("PUT", `/api/v1/files/${path}`, {
      json: { content },
    });
  }

  async delete(path: string): Promise<void> {
    await this.transport.request("DELETE", `/api/v1/files/${path}`);
  }

  async upload(
    path: string,
    file: File | Blob,
    filename?: string,
  ): Promise<{ path: string; filename: string; size: number; status: string }> {
    const formData = new FormData();
    formData.append("path", path);
    formData.append("file", file, filename);
    return this.transport.request("POST", "/api/v1/files/upload", {
      body: formData,
    });
  }
}
