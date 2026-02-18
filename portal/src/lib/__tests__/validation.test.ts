import { describe, it, expect } from "vitest";
import { validateName } from "../validation";

describe("validateName", () => {
  it("returns null for valid lowercase slugs", () => {
    expect(validateName("orders")).toBeNull();
    expect(validateName("my-pipeline")).toBeNull();
    expect(validateName("my_pipeline")).toBeNull();
    expect(validateName("bronze01")).toBeNull();
    expect(validateName("a")).toBeNull();
    expect(validateName("x-y_z")).toBeNull();
  });

  it("returns null for empty string (handled by required checks)", () => {
    expect(validateName("")).toBeNull();
  });

  it("rejects names starting with a digit", () => {
    const err = validateName("1orders");
    expect(err).toBe("Must start with a lowercase letter");
  });

  it("rejects names starting with a hyphen", () => {
    const err = validateName("-orders");
    expect(err).toBe("Must start with a lowercase letter");
  });

  it("rejects names starting with an underscore", () => {
    const err = validateName("_orders");
    expect(err).toBe("Must start with a lowercase letter");
  });

  it("rejects names starting with uppercase", () => {
    const err = validateName("Orders");
    expect(err).toBe("Must start with a lowercase letter");
  });

  it("rejects names with uppercase letters", () => {
    const err = validateName("myPipeline");
    expect(err).toBe(
      "Only lowercase letters, digits, hyphens, and underscores allowed"
    );
  });

  it("rejects names with spaces", () => {
    const err = validateName("my pipeline");
    expect(err).toBe(
      "Only lowercase letters, digits, hyphens, and underscores allowed"
    );
  });

  it("rejects names with special characters", () => {
    expect(validateName("my.pipeline")).toBe(
      "Only lowercase letters, digits, hyphens, and underscores allowed"
    );
    expect(validateName("my@pipeline")).toBe(
      "Only lowercase letters, digits, hyphens, and underscores allowed"
    );
    expect(validateName("my!pipeline")).toBe(
      "Only lowercase letters, digits, hyphens, and underscores allowed"
    );
  });

  it("rejects names longer than 128 characters", () => {
    const long = "a" + "b".repeat(128); // 129 chars
    expect(validateName(long)).toBe("Name must be at most 128 characters");
  });

  it("accepts names exactly 128 characters", () => {
    const exact = "a" + "b".repeat(127); // 128 chars
    expect(validateName(exact)).toBeNull();
  });
});
