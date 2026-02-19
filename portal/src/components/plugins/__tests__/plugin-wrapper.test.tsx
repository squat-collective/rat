// @vitest-environment jsdom
import React from "react";
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { PluginWrapper } from "../plugin-wrapper";

describe("PluginWrapper", () => {
  it("renders children directly when no plugin package is configured", () => {
    render(
      <PluginWrapper>
        <div>App Content</div>
      </PluginWrapper>,
    );
    expect(screen.getByText("App Content")).toBeDefined();
  });
});
