"use client";

import { SessionProvider as AuthSessionProvider } from "@/lib/auth/client";

/**
 * Client-side session provider wrapper.
 * Community edition: pass-through (no auth).
 * Pro edition: real NextAuth SessionProvider (injected by plugin overlay).
 */
export function SessionProvider({ children }: { children: React.ReactNode }) {
  return <AuthSessionProvider>{children}</AuthSessionProvider>;
}
