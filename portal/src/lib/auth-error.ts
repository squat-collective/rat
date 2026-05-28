/**
 * Duck-type check for authentication errors.
 *
 * Works with RatError/AuthenticationError from the SDK, plain Error objects
 * with statusCode, or any object shaped like { name, statusCode }.
 * This lets community code detect auth failures without importing next-auth.
 */
export function isAuthError(error: unknown): boolean {
  if (!error || typeof error !== "object") return false;

  const err = error as Record<string, unknown>;

  if (err.name === "AuthenticationError") return true;
  if (err.statusCode === 401) return true;

  return false;
}
