import { describe, it, expect } from "vitest";
import { formatBytes } from "../utils";

describe("formatBytes", () => {
  it("returns '0 B' for zero bytes", () => {
    expect(formatBytes(0)).toBe("0 B");
  });

  it("formats bytes without decimals", () => {
    expect(formatBytes(1)).toBe("1 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1023)).toBe("1023 B");
  });

  it("formats kilobytes with one decimal", () => {
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(10240)).toBe("10.0 KB");
  });

  it("formats megabytes with one decimal", () => {
    expect(formatBytes(1048576)).toBe("1.0 MB");
    expect(formatBytes(1572864)).toBe("1.5 MB");
  });

  it("formats gigabytes with one decimal", () => {
    expect(formatBytes(1073741824)).toBe("1.0 GB");
    expect(formatBytes(2684354560)).toBe("2.5 GB");
  });

  it("handles exact power-of-1024 boundaries", () => {
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1024 * 1024)).toBe("1.0 MB");
    expect(formatBytes(1024 * 1024 * 1024)).toBe("1.0 GB");
  });

  it("rounds to one decimal place for KB/MB/GB", () => {
    // 1500 bytes = 1.46... KB -> rounds to 1.5 KB
    expect(formatBytes(1500)).toBe("1.5 KB");
  });
});
