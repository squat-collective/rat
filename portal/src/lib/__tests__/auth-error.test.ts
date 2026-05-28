import { describe, it, expect } from "vitest";
import { isAuthError } from "../auth-error";

describe("isAuthError", () => {
  it("returns true for AuthenticationError name", () => {
    const err = new Error("unauthorized");
    err.name = "AuthenticationError";
    expect(isAuthError(err)).toBe(true);
  });

  it("returns true for statusCode 401", () => {
    const err = Object.assign(new Error("bad token"), { statusCode: 401 });
    expect(isAuthError(err)).toBe(true);
  });

  it("returns true for plain object with statusCode 401", () => {
    expect(isAuthError({ statusCode: 401, message: "invalid token" })).toBe(true);
  });

  it("returns false for non-auth errors", () => {
    expect(isAuthError(new Error("network error"))).toBe(false);
    expect(isAuthError({ statusCode: 500 })).toBe(false);
    expect(isAuthError({ name: "SomeOtherError" })).toBe(false);
  });

  it("returns false for null/undefined/primitives", () => {
    expect(isAuthError(null)).toBe(false);
    expect(isAuthError(undefined)).toBe(false);
    expect(isAuthError("string")).toBe(false);
    expect(isAuthError(42)).toBe(false);
  });
});
