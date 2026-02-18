import { describe, it, expect } from "vitest";
import {
  LAYER_BADGE_COLORS,
  STATUS_COLORS,
  STATUS_EMOJI,
  RAT_LOGO,
} from "../constants";

describe("LAYER_BADGE_COLORS", () => {
  it("defines colors for bronze layer", () => {
    expect(LAYER_BADGE_COLORS["bronze"]).toBeDefined();
    expect(LAYER_BADGE_COLORS["bronze"]).toContain("orange");
  });

  it("defines colors for silver layer", () => {
    expect(LAYER_BADGE_COLORS["silver"]).toBeDefined();
    expect(LAYER_BADGE_COLORS["silver"]).toContain("zinc");
  });

  it("defines colors for gold layer", () => {
    expect(LAYER_BADGE_COLORS["gold"]).toBeDefined();
    expect(LAYER_BADGE_COLORS["gold"]).toContain("yellow");
  });

  it("returns undefined for unknown layers", () => {
    expect(LAYER_BADGE_COLORS["platinum"]).toBeUndefined();
  });
});

describe("STATUS_COLORS", () => {
  const expectedStatuses = ["success", "failed", "running", "pending", "cancelled"];

  for (const status of expectedStatuses) {
    it(`defines colors for ${status} status`, () => {
      expect(STATUS_COLORS[status]).toBeDefined();
      expect(typeof STATUS_COLORS[status]).toBe("string");
    });
  }

  it("returns undefined for unknown status", () => {
    expect(STATUS_COLORS["unknown"]).toBeUndefined();
  });
});

describe("STATUS_EMOJI", () => {
  it("defines emoji for all standard statuses", () => {
    expect(STATUS_EMOJI["success"]).toBeDefined();
    expect(STATUS_EMOJI["failed"]).toBeDefined();
    expect(STATUS_EMOJI["running"]).toBeDefined();
    expect(STATUS_EMOJI["pending"]).toBeDefined();
    expect(STATUS_EMOJI["cancelled"]).toBeDefined();
  });

  it("each emoji is a non-empty string", () => {
    for (const [, emoji] of Object.entries(STATUS_EMOJI)) {
      expect(typeof emoji).toBe("string");
      expect(emoji.length).toBeGreaterThan(0);
    }
  });
});

describe("RAT_LOGO", () => {
  it("is a non-empty string", () => {
    expect(typeof RAT_LOGO).toBe("string");
    expect(RAT_LOGO.length).toBeGreaterThan(0);
  });

  it("contains RAT text", () => {
    // The ASCII art should contain fragments of "R A T"
    expect(RAT_LOGO).toContain("██");
  });

  it("is multi-line", () => {
    const lines = RAT_LOGO.split("\n");
    expect(lines.length).toBeGreaterThan(3);
  });
});
