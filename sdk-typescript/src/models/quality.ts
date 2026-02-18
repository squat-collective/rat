export interface QualityTest {
  name: string;
  sql: string;
  severity: string;
  description: string;
  published: boolean;
  tags: string[];
  remediation: string;
}

export interface QualityTestListResponse {
  tests: QualityTest[];
  total: number;
}

export interface CreateQualityTestRequest {
  name: string;
  sql: string;
  severity?: string;
  description?: string;
}

export interface CreateQualityTestResponse {
  name: string;
  severity: string;
  path: string;
}

export interface QualityTestResult {
  name: string;
  status: string;
  severity: string;
  value: number;
  expected: number;
  duration_ms: number;
}

export interface QualityRunResponse {
  results: QualityTestResult[];
  passed: number;
  failed: number;
  total: number;
}
