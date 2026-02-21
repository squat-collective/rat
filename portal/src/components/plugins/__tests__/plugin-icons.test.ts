// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { getPluginIcon } from "../plugin-icons";
import { Puzzle } from "lucide-react";

describe("getPluginIcon", () => {
  it("resolves PascalCase icon name", () => {
    const Icon = getPluginIcon("Shield");
    expect(Icon).not.toBe(Puzzle);
    expect(Icon.displayName).toBe("Shield");
  });

  it("resolves kebab-case icon name", () => {
    const Icon = getPluginIcon("shield-check");
    expect(Icon).not.toBe(Puzzle);
    expect(Icon.displayName).toBe("ShieldCheck");
  });

  it("resolves camelCase icon name", () => {
    const Icon = getPluginIcon("activity");
    expect(Icon).not.toBe(Puzzle);
    expect(Icon.displayName).toBe("Activity");
  });

  it("returns Puzzle fallback for unknown icon name", () => {
    const Icon = getPluginIcon("nonexistent-icon-xyz");
    expect(Icon.displayName).toBe("Puzzle");
  });

  it("returns Puzzle fallback for empty string", () => {
    const Icon = getPluginIcon("");
    expect(Icon.displayName).toBe("Puzzle");
  });
});
