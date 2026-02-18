export interface Webhook {
  id: string;
  pipeline_id: string;
  url: string;
  token: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface WebhookListResponse {
  webhooks: Webhook[];
  total: number;
}

export interface CreateWebhookRequest {
  pipeline_id: string;
  url: string;
}

export interface CreateWebhookResponse {
  id: string;
  url: string;
  token: string;
}
