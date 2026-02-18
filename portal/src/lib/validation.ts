const NAME_RE = /^[a-z][a-z0-9_-]*$/;

/**
 * Validates a resource name (pipeline, namespace, landing zone, quality test).
 * Returns an error message if invalid, or null if valid.
 * Empty values return null — emptiness should be handled by required checks.
 */
export function validateName(value: string): string | null {
  if (!value) return null;
  if (value.length > 128) return "Name must be at most 128 characters";
  if (!/^[a-z]/.test(value)) return "Must start with a lowercase letter";
  if (!NAME_RE.test(value))
    return "Only lowercase letters, digits, hyphens, and underscores allowed";
  return null;
}
