// @vitest-environment jsdom
import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { ErrorAlert } from "../error-alert";

// Mock lucide-react to avoid SVG rendering issues in jsdom
vi.mock("lucide-react", () => ({
  AlertTriangle: () => <span data-testid="alert-icon" />,
}));

describe("ErrorAlert", () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  // NOTE: the auth-aware "Session expired / SIGN IN" behaviour was removed
  // when auth moved to the adapter layer. ErrorAlert is now a pure display
  // component; auth-error handling lives in @/lib/auth/client.

  it("shows normal error message for non-auth errors", () => {
    render(<ErrorAlert error={new Error("Something broke")} />);

    expect(screen.getByText("Something broke")).toBeDefined();
    expect(screen.queryByText("SIGN IN")).toBeNull();
  });

  it("shows prefix with normal errors", () => {
    render(<ErrorAlert error="Connection failed" prefix="QUERY" />);

    expect(screen.getByText("QUERY:")).toBeDefined();
    expect(screen.getByText("Connection failed")).toBeDefined();
  });

  it("shows fallback message for unknown error types", () => {
    render(<ErrorAlert error={{ weird: true }} />);

    expect(screen.getByText("An unexpected error occurred")).toBeDefined();
  });

  it("extracts message from plain objects with .message property", () => {
    render(<ErrorAlert error={{ message: "server error", statusCode: 500 }} />);

    expect(screen.getByText("server error")).toBeDefined();
  });

  it("extracts message from plain objects with .error property", () => {
    render(<ErrorAlert error={{ error: "list grants: unimplemented" }} />);

    expect(screen.getByText("list grants: unimplemented")).toBeDefined();
  });
});
