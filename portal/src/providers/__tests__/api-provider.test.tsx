// @vitest-environment jsdom
import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, cleanup } from "@testing-library/react";
import { ApiProvider } from "../api-provider";

// Mock auth client
const mockSignOut = vi.fn();
const mockUseAuthSession = vi.fn();

vi.mock("@/lib/auth/client", () => ({
  useAuthSession: () => mockUseAuthSession(),
  signOut: (...args: unknown[]) => mockSignOut(...args),
}));

// Mock SWR to capture config
let capturedSwrConfig: Record<string, unknown> = {};
vi.mock("swr", () => ({
  SWRConfig: ({
    children,
    value,
  }: {
    children: React.ReactNode;
    value: Record<string, unknown>;
  }) => {
    capturedSwrConfig = value;
    return <>{children}</>;
  },
}));

// Mock RatClient
vi.mock("@squat-collective/rat-client", () => ({
  RatClient: vi.fn().mockImplementation(() => ({})),
}));

vi.mock("@/lib/api-client", () => ({
  PUBLIC_API_URL: "http://localhost:8080",
}));

describe("ApiProvider", () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();
    capturedSwrConfig = {};
    mockUseAuthSession.mockReturnValue({ data: null, status: "unauthenticated" });
  });

  it("does not auto-signOut on SWR 401 errors", () => {
    // SWR 401s are handled by ErrorAlert (manual SIGN IN button),
    // not by automatic signOut — avoids nuking valid sessions on transient 401s
    mockUseAuthSession.mockReturnValue({
      data: { accessToken: "tok" },
      status: "authenticated",
    });

    render(
      <ApiProvider>
        <div>child</div>
      </ApiProvider>,
    );

    const onError = capturedSwrConfig.onError as (err: unknown) => void;
    const authError = Object.assign(new Error("unauthorized"), {
      statusCode: 401,
    });
    onError(authError);

    expect(mockSignOut).not.toHaveBeenCalled();
  });

  it("does not call signOut for non-auth errors", () => {
    mockUseAuthSession.mockReturnValue({
      data: { accessToken: "tok" },
      status: "authenticated",
    });

    render(
      <ApiProvider>
        <div>child</div>
      </ApiProvider>,
    );

    const onError = capturedSwrConfig.onError as (err: unknown) => void;
    onError(new Error("network error"));

    expect(mockSignOut).not.toHaveBeenCalled();
  });

  it("triggers signOut when session.error is RefreshTokenError", () => {
    mockUseAuthSession.mockReturnValue({
      data: { error: "RefreshTokenError" },
      status: "authenticated",
    });

    render(
      <ApiProvider>
        <div>child</div>
      </ApiProvider>,
    );

    expect(mockSignOut).toHaveBeenCalledWith({ callbackUrl: "/login" });
  });

  it("does not signOut on RefreshTokenError when unauthenticated", () => {
    mockUseAuthSession.mockReturnValue({
      data: { error: "RefreshTokenError" },
      status: "unauthenticated",
    });

    render(
      <ApiProvider>
        <div>child</div>
      </ApiProvider>,
    );

    expect(mockSignOut).not.toHaveBeenCalled();
  });

  it("skips retry on auth errors via onErrorRetry", () => {
    render(
      <ApiProvider>
        <div>child</div>
      </ApiProvider>,
    );

    const onErrorRetry = capturedSwrConfig.onErrorRetry as (
      err: unknown,
      key: string,
      config: unknown,
      revalidate: (opts: { retryCount: number }) => void,
      opts: { retryCount: number },
    ) => void;

    const revalidate = vi.fn();
    const authError = Object.assign(new Error("unauthorized"), {
      statusCode: 401,
    });

    onErrorRetry(authError, "/api/test", {}, revalidate, { retryCount: 0 });

    // Should NOT retry for auth errors
    expect(revalidate).not.toHaveBeenCalled();
  });

  it("renders nothing while auth session is loading", () => {
    mockUseAuthSession.mockReturnValue({ data: null, status: "loading" });

    const { container } = render(
      <ApiProvider>
        <div data-testid="child">child</div>
      </ApiProvider>,
    );

    // Children should not be rendered during loading
    expect(container.innerHTML).toBe("");
    // SWR config should not have been set (no SWRConfig rendered)
    expect(capturedSwrConfig).toEqual({});
  });
});
