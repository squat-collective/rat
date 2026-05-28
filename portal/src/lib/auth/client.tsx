"use client";

/**
 * Noop auth client adapter (community edition).
 *
 * SessionProvider is a pass-through wrapper. useAuthSession returns an
 * unauthenticated stub. In Pro builds, this file is replaced by the
 * auth-keycloak plugin overlay that re-exports from next-auth/react.
 */

/** Minimal session shape matching NextAuth's Session (client-side). */
interface AuthSession {
  user?: { name?: string | null; email?: string | null; image?: string | null };
  accessToken?: string;
  userId?: string;
  expires?: string;
}

export function SessionProvider({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}

export function useAuthSession(): {
  data: AuthSession | null;
  status: "authenticated" | "loading" | "unauthenticated";
} {
  return { data: null, status: "unauthenticated" };
}
