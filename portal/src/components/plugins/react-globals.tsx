"use client";

import { useEffect } from "react";
import React from "react";
import ReactDOM from "react-dom";

/**
 * Exposes React and ReactDOM on `window` so plugin bundles can use them
 * without bundling their own copy. Must render before any plugin scripts load.
 */
export function ReactGlobals() {
  useEffect(() => {
    (window as any).React = React;
    (window as any).ReactDOM = ReactDOM;
  }, []);
  return null;
}
