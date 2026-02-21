"use client";

import { useVerbs } from "@/hooks/use-api";
import { Loading } from "@/components/loading";
import { ErrorAlert } from "@/components/error-alert";
import { Badge } from "@/components/ui/badge";
import { ArrowRight } from "lucide-react";

const verbColors: Record<string, string> = {
  admin: "bg-red-500/20 text-red-400 border-red-500/30",
  write: "bg-orange-500/20 text-orange-400 border-orange-500/30",
  read: "bg-green-500/20 text-green-400 border-green-500/30",
  execute: "bg-blue-500/20 text-blue-400 border-blue-500/30",
  publish: "bg-purple-500/20 text-purple-400 border-purple-500/30",
  delete: "bg-red-500/20 text-red-300 border-red-500/30",
};

function VerbBadge({ verb }: { verb: string }) {
  const color = verbColors[verb] ?? "bg-muted text-muted-foreground border-border";
  return (
    <Badge variant="outline" className={`text-[10px] ${color}`}>
      {verb}
    </Badge>
  );
}

export function PermissionsVerbs() {
  const { data, isLoading, error } = useVerbs();

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">
        Registered verbs and their implication chains. Verbs are seeded by the
        engine — granting a verb automatically grants all verbs it implies.
      </p>

      {isLoading ? (
        <Loading text="Loading verbs..." />
      ) : error ? (
        <ErrorAlert error={error} prefix="Failed to load verbs" />
      ) : !data?.verbs?.length ? (
        <div className="brutal-card p-6 text-center">
          <p className="text-xs text-muted-foreground">
            No verbs registered yet.
          </p>
        </div>
      ) : (
        <div className="brutal-card overflow-hidden">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-border bg-muted/30">
                <th className="text-left p-2 text-[10px] tracking-wider text-muted-foreground">
                  Verb
                </th>
                <th className="text-left p-2 text-[10px] tracking-wider text-muted-foreground">
                  Implies
                </th>
                <th className="text-left p-2 text-[10px] tracking-wider text-muted-foreground">
                  Description
                </th>
              </tr>
            </thead>
            <tbody>
              {data.verbs.map((verb, i) => (
                <tr
                  key={verb.name}
                  className={`border-b border-border/50 ${i % 2 === 0 ? "" : "bg-muted/10"}`}
                >
                  <td className="p-2">
                    <VerbBadge verb={verb.name} />
                  </td>
                  <td className="p-2">
                    {verb.implies?.length ? (
                      <div className="flex items-center gap-1 flex-wrap">
                        <ArrowRight className="h-3 w-3 text-muted-foreground shrink-0" />
                        {verb.implies.map((implied) => (
                          <VerbBadge key={implied} verb={implied} />
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className="p-2 text-muted-foreground">
                    {verb.description || "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
