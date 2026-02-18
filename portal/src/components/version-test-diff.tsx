"use client";

import type { PipelineVersion } from "@squat-collective/rat-client";
import { Badge } from "@/components/ui/badge";

interface VersionTestDiffProps {
  current: PipelineVersion;
  previous: PipelineVersion | undefined;
}

function extractTestNames(versions: Record<string, string>): Set<string> {
  const tests = new Set<string>();
  for (const key of Object.keys(versions)) {
    const match = key.match(/\/tests\/quality\/(.+)\.sql$/);
    if (match) {
      tests.add(match[1]);
    }
  }
  return tests;
}

export function VersionTestDiff({ current, previous }: VersionTestDiffProps) {
  const currentVersions = current.published_versions ?? {};
  const previousVersions = previous?.published_versions ?? {};

  const currentTests = extractTestNames(currentVersions);
  const previousTests = extractTestNames(previousVersions);

  const added: string[] = [];
  const removed: string[] = [];
  const modified: string[] = [];

  for (const name of currentTests) {
    if (!previousTests.has(name)) {
      added.push(name);
    }
  }

  for (const name of previousTests) {
    if (!currentTests.has(name)) {
      removed.push(name);
    }
  }

  // Check modified: same key exists in both but version ID changed
  for (const [key, vid] of Object.entries(currentVersions)) {
    if (!key.match(/\/tests\/quality\/.+\.sql$/)) continue;
    const prevVid = previousVersions[key];
    if (prevVid && prevVid !== vid) {
      const match = key.match(/\/tests\/quality\/(.+)\.sql$/);
      if (match) modified.push(match[1]);
    }
  }

  if (added.length === 0 && removed.length === 0 && modified.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-wrap gap-1 mt-1">
      {added.map((name) => (
        <Badge key={`add-${name}`} variant="outline" className="text-[9px] border-green-500/50 text-green-400 gap-1">
          + {name}
        </Badge>
      ))}
      {removed.map((name) => (
        <Badge key={`rm-${name}`} variant="outline" className="text-[9px] border-destructive/50 text-destructive gap-1">
          - {name}
        </Badge>
      ))}
      {modified.map((name) => (
        <Badge key={`mod-${name}`} variant="outline" className="text-[9px] border-yellow-500/50 text-yellow-500 gap-1">
          ~ {name}
        </Badge>
      ))}
    </div>
  );
}
