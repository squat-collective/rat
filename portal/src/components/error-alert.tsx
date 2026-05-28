import { AlertTriangle } from "lucide-react";

interface ErrorAlertProps {
  /** Error message to display. Accepts Error objects or strings. */
  error: Error | string | unknown;
  /** Optional prefix text before the error message. */
  prefix?: string;
}

/**
 * Displays an API/SWR error in the RAT underground aesthetic.
 * Uses the existing error-block CSS class for consistent styling
 * with the scanning red line animation.
 */
/** Best-effort human message from whatever the API/SWR threw. */
function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  // RAT API errors often arrive as plain objects: {message} or {error}.
  if (error && typeof error === "object") {
    const o = error as Record<string, unknown>;
    if (typeof o.message === "string") return o.message;
    if (typeof o.error === "string") return o.error;
  }
  return "An unexpected error occurred";
}

export function ErrorAlert({ error, prefix }: ErrorAlertProps) {
  const message = errorMessage(error);

  return (
    <div className="error-block px-4 py-3 flex items-start gap-2">
      <AlertTriangle className="h-3.5 w-3.5 text-destructive shrink-0 mt-0.5" />
      <div className="text-xs text-destructive">
        {prefix && <span className="font-bold tracking-wider">{prefix}: </span>}
        {message}
      </div>
    </div>
  );
}
