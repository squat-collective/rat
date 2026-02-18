export interface HealthResponse {
  status: string;
}

export interface PluginFeature {
  enabled: boolean;
  type?: string;
}

export interface LicenseInfo {
  valid: boolean;
  tier?: string;
  org_id?: string;
  plugins?: string[];
  seat_limit?: number;
  expires_at?: string; // RFC3339
  error?: string;
}

export interface FeaturesResponse {
  edition: string;
  namespaces: boolean;
  multi_user: boolean;
  landing_zones: boolean;
  plugins: Record<string, PluginFeature>;
  license?: LicenseInfo;
}
