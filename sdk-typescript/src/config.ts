/** Interceptor called before each request. Can modify headers or path. */
export interface RequestInterceptor {
  (request: { method: string; url: string; headers: Record<string, string> }): void | Promise<void>;
}

/** Interceptor called after each response. Can inspect or transform. */
export interface ResponseInterceptor {
  (response: Response, request: { method: string; url: string }): void | Promise<void>;
}

export interface ClientConfig {
  apiUrl: string;
  timeout?: number;
  maxRetries?: number;
  headers?: Record<string, string>;
  /** Interceptors called before each outgoing request. */
  onRequest?: RequestInterceptor[];
  /** Interceptors called after each response (before error handling). */
  onResponse?: ResponseInterceptor[];
}

export const DEFAULT_CONFIG: Required<
  Pick<ClientConfig, "timeout" | "maxRetries">
> = {
  timeout: 30_000,
  maxRetries: 3,
};
