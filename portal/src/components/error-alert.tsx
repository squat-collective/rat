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
export function ErrorAlert({ error, prefix }: ErrorAlertProps) {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === "string"
        ? error
        : "An unexpected error occurred";

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
