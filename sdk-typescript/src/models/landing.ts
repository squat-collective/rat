export interface LandingZone {
  id: string;
  namespace: string;
  name: string;
  description: string;
  owner: string | null;
  expected_schema: string;
  file_count: number;
  total_bytes: number;
  created_at: string;
  updated_at: string;
}

export interface UpdateLandingZoneRequest {
  description?: string;
  owner?: string | null;
  expected_schema?: string;
}

export interface LandingFile {
  id: string;
  zone_id: string;
  filename: string;
  s3_path: string;
  size_bytes: number;
  content_type: string;
  uploaded_by: string | null;
  uploaded_at: string;
}

export interface LandingZoneListResponse {
  zones: LandingZone[];
  total: number;
}

export interface LandingFileListResponse {
  files: LandingFile[];
  total: number;
}

export interface CreateLandingZoneRequest {
  namespace: string;
  name: string;
  description?: string;
}

export interface SampleFileInfo {
  path: string;
  size: number;
  modified: string;
  type: string;
}

export interface SampleFileListResponse {
  files: SampleFileInfo[];
  total: number;
}

export interface SampleFileUploadResponse {
  path: string;
  filename: string;
  size: number;
  status: string;
}
