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

  it("does not signOut on RefreshTokenError (handled by the auth adapter, not here)", () => {
    // Auto-signOut + token-refresh handling moved out of ApiProvider into
    // the auth adapter (@/lib/auth/client). ApiProvider no longer reacts to
    // session.error — it just stops injecting a Bearer header.
    mockUseAuthSession.mockReturnValue({
      data: { error: "RefreshTokenError" },
      status: "authenticated",
    });

    render(
      <ApiProvider>
        <div>child</div>
      </ApiProvider>,
    );

    expect(mockSignOut).not.toHaveBeenCalled();
  });

  it("renders children immediately (no session-loading gate)", () => {
    // ApiProvider used to render nothing while the session loaded; that gate
    // was removed when auth became an adapter, so children render right away
    // and SWRConfig is always mounted.
    mockUseAuthSession.mockReturnValue({ data: null, status: "loading" });

    const { getByTestId } = render(
      <ApiProvider>
        <div data-testid="child">child</div>
      </ApiProvider>,
    );

    expect(getByTestId("child")).toBeTruthy();
    expect(typeof capturedSwrConfig.onError).toBe("function");
  });
});
