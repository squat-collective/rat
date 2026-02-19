/**
 * Noop auth adapter (community edition).
 *
 * Auth is disabled — all exports are stubs. In Pro builds, this file is
 * replaced by the auth-keycloak plugin overlay with real NextAuth config.
 */
import { NextResponse } from "next/server";

/** Minimal session shape matching NextAuth's Session. */
export interface AuthSession {
  user?: { name?: string | null; email?: string | null; image?: string | null };
  accessToken?: string;
  userId?: string;
  expires?: string;
}

export const authEnabled = false;

export async function auth(): Promise<AuthSession | null> {
  return null;
}

export async function signIn(): Promise<never> {
  throw new Error("Auth not configured");
}

export async function signOut(): Promise<never> {
  throw new Error("Auth not configured");
}

export const handlers = {
  GET: () =>
    NextResponse.json({ error: "Auth not configured" }, { status: 404 }),
  POST: () =>
    NextResponse.json({ error: "Auth not configured" }, { status: 404 }),
};
