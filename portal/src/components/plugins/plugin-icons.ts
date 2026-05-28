import { type LucideIcon, Puzzle } from "lucide-react";
import * as icons from "lucide-react";

/**
 * Resolves a string icon name from a plugin descriptor to a lucide-react component.
 * Accepts kebab-case ("shield-check"), camelCase ("shieldCheck"), or PascalCase ("ShieldCheck").
 * Falls back to the Puzzle icon when the name doesn't match any known icon.
 */
export function getPluginIcon(name: string): LucideIcon {
  const pascal = toPascalCase(name);
  const candidate = (icons as Record<string, unknown>)[pascal];
  // lucide-react icons are forwardRef components (objects with $$typeof + render),
  // not plain functions. Check for the displayName property that all icons have.
  if (candidate && typeof candidate === "object" && "displayName" in candidate) {
    return candidate as LucideIcon;
  }
  return Puzzle;
}

/** Converts kebab-case or camelCase to PascalCase. */
function toPascalCase(s: string): string {
  return s
    .replace(/(^|[-_])(\w)/g, (_, __, c: string) => c.toUpperCase());
}
