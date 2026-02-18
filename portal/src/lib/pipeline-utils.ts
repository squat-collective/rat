/**
 * Extract landing_zone('...') references from pipeline source code.
 * Returns unique zone names in order of first appearance.
 */
export function extractLandingZones(code: string): string[] {
  const regex = /landing_zone\(\s*['"]([^'"]+)['"]\s*\)/g;
  const zones: string[] = [];
  let match: RegExpExecArray | null;
  while ((match = regex.exec(code)) !== null) {
    if (!zones.includes(match[1])) zones.push(match[1]);
  }
  return zones;
}
