"use client";

import { useEffect } from "react";
import React from "react";
import ReactDOM from "react-dom";
import { pluginUIKit } from "./plugin-ui-kit";

/**
 * Exposes React, ReactDOM and a small UI kit (window.__RAT_UI) on `window` so
 * plugin bundles can reuse them without bundling their own copy. Must render
 * before any plugin scripts load.
 */
export function ReactGlobals() {
  useEffect(() => {
    (window as any).React = React;
    (window as any).ReactDOM = ReactDOM;
    (window as any).__RAT_UI = pluginUIKit;
  }, []);
  return null;
}
