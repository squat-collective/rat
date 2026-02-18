import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// We need to mock fetch before importing the module
const mockFetch = vi.fn();
vi.stubGlobal("fetch", mockFetch);

// Reset environment variables to control SERVER_API_URL
const originalEnv = { ...process.env };

describe("serverApi", () => {
  beforeEach(() => {
    vi.resetModules();
    mockFetch.mockReset();
  });

  afterEach(() => {
    process.env = { ...originalEnv };
  });

  function createMockResponse<T>(data: T, ok = true, status = 200) {
    return {
      ok,
      status,
      statusText: ok ? "OK" : "Internal Server Error",
      json: () => Promise.resolve(data),
    };
  }

  it("fetches pipelines list from /api/v1/pipelines", async () => {
    const mockData = { pipelines: [] };
    mockFetch.mockResolvedValueOnce(createMockResponse(mockData));

    const { serverApi } = await import("../server-api");
    const result = await serverApi.pipelines.list();

    expect(mockFetch).toHaveBeenCalledTimes(1);
    const [url, options] = mockFetch.mock.calls[0];
    expect(url).toContain("/api/v1/pipelines");
    expect(options.next?.revalidate).toBeGreaterThan(0);
    expect(result).toEqual(mockData);
  });

  it("fetches runs list from /api/v1/runs", async () => {
    const mockData = { runs: [] };
    mockFetch.mockResolvedValueOnce(createMockResponse(mockData));

    const { serverApi } = await import("../server-api");
    const result = await serverApi.runs.list();

    expect(mockFetch).toHaveBeenCalledTimes(1);
    const [url] = mockFetch.mock.calls[0];
    expect(url).toContain("/api/v1/runs");
    expect(result).toEqual(mockData);
  });

  it("fetches tables list from /api/v1/tables", async () => {
    const mockData = { tables: [] };
    mockFetch.mockResolvedValueOnce(createMockResponse(mockData));

    const { serverApi } = await import("../server-api");
    const result = await serverApi.tables.list();

    const [url] = mockFetch.mock.calls[0];
    expect(url).toContain("/api/v1/tables");
    expect(result).toEqual(mockData);
  });

  it("fetches features from /api/v1/features", async () => {
    const mockData = { features: [] };
    mockFetch.mockResolvedValueOnce(createMockResponse(mockData));

    const { serverApi } = await import("../server-api");
    const result = await serverApi.features();

    const [url] = mockFetch.mock.calls[0];
    expect(url).toContain("/api/v1/features");
    expect(result).toEqual(mockData);
  });

  it("throws error when API returns non-ok response", async () => {
    mockFetch.mockResolvedValueOnce(createMockResponse(null, false, 500));

    const { serverApi } = await import("../server-api");
    await expect(serverApi.pipelines.list()).rejects.toThrow(
      "API /api/v1/pipelines: 500 Internal Server Error",
    );
  });

  it("uses ISR revalidation cache strategy for all requests", async () => {
    mockFetch.mockResolvedValueOnce(createMockResponse({ tables: [] }));

    const { serverApi } = await import("../server-api");
    await serverApi.tables.list();

    const [, options] = mockFetch.mock.calls[0];
    expect(options.next?.revalidate).toBeGreaterThan(0);
  });

  it("defaults to localhost:8080 when no env vars are set", async () => {
    delete process.env.API_URL;
    delete process.env.NEXT_PUBLIC_API_URL;

    mockFetch.mockResolvedValueOnce(createMockResponse({ pipelines: [] }));

    const { serverApi } = await import("../server-api");
    await serverApi.pipelines.list();

    const [url] = mockFetch.mock.calls[0];
    expect(url).toBe("http://localhost:8080/api/v1/pipelines");
  });
});
