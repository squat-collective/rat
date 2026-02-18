import { Skeleton } from "@/components/ui/skeleton";

export function Loading({ text = "Loading..." }: { text?: string }) {
  return (
    <div className="flex items-center gap-3 py-8">
      <div className="h-3 w-3 border border-primary animate-spin" />
      <span className="text-xs text-muted-foreground tracking-wider">
        {text}<span className="animate-pulse-neon">_</span>
      </span>
    </div>
  );
}

export function PageSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-6 w-48" />
      <Skeleton className="h-3 w-96" />
      <div className="space-y-2 pt-4">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-8 w-full" />
        ))}
      </div>
    </div>
  );
}
