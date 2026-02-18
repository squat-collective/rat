export interface FileInfo {
  path: string;
  size: number;
  modified: string;
  type: string;
}

export interface FileContent {
  path: string;
  content: string;
  size: number;
  modified: string;
}

export interface FileListResponse {
  files: FileInfo[];
}
