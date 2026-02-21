// @vitest-environment jsdom
import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { ErrorAlert } from "../error-alert";

const mockSignOut = vi.fn();

vi.mock("@/lib/auth/client", () => ({
  signOut: (...args: unknown[]) => mockSignOut(...args),
}));

// Mock lucide-react to avoid SVG rendering issues in jsdom
vi.mock("lucide-react", () => ({
  AlertTriangle: () => <span data-testid="alert-icon" />,
}));

describe("ErrorAlert", () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows SIGN IN button for auth errors", () => {
    const authError = Object.assign(new Error("invalid token"), {
      statusCode: 401,
    });

    render(<ErrorAlert error={authError} />);

    expect(screen.getByText("Session expired")).toBeDefined();
    expect(screen.getByText("SIGN IN")).toBeDefined();
  });

  it("calls signOut when SIGN IN is clicked", () => {
    const authError = Object.assign(new Error("invalid token"), {
      name: "AuthenticationError",
    });

    render(<ErrorAlert error={authError} />);

    fireEvent.click(screen.getByText("SIGN IN"));
    expect(mockSignOut).toHaveBeenCalledWith({ callbackUrl: "/login" });
  });

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
