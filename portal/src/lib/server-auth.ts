/**
 * Server-side auth helpers for server components and server actions.
 *
 * getServerApi() returns an authenticated API client when auth is enabled,
 * or falls back to the unauthenticated serverApi for community edition.
 */
import { auth } from "@/lib/auth/server";
import { serverApi, createAuthServerApi } from "@/lib/server-api";

/** Re-export auth() as getSession for convenience. */
export const getSession = auth;

/**
 * Get a server-side API client with auth when available.
 * - Auth enabled + session: returns authenticated client with Bearer token
 * - Auth disabled or no session: returns unauthenticated serverApi (community mode)
 */
export async function getServerApi() {
  const session = await auth();
  if (session?.accessToken) {
    return createAuthServerApi(session.accessToken);
  }
  return serverApi;
}
