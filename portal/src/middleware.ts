import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";
import { auth, authEnabled } from "@/lib/auth/server";

/** Paths that don't require authentication. */
const PUBLIC_PATHS = ["/login", "/api/auth"];

function isPublicPath(pathname: string): boolean {
  return PUBLIC_PATHS.some(
    (p) => pathname === p || pathname.startsWith(`${p}/`),
  );
}

function buildCsp(nonce: string, isDev: boolean): string {
  const keycloakOrigin = process.env.KEYCLOAK_ISSUER
    ? new URL(process.env.KEYCLOAK_ISSUER).origin
    : "";

  return [
    `script-src 'self' 'nonce-${nonce}'${isDev ? " 'unsafe-eval'" : ""}`,
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    "font-src 'self'",
    `connect-src 'self' ${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"}${isDev ? " ws://localhost:3000 wss://localhost:3000" : ""}`,
    "worker-src 'self' blob:",
    `frame-src${keycloakOrigin ? ` ${keycloakOrigin}` : " 'none'"}`,
    "frame-ancestors 'none'",
    "base-uri 'self'",
    `form-action 'self'${keycloakOrigin ? ` ${keycloakOrigin}` : ""}`,
  ].join("; ");
}

async function handleMiddleware(request: NextRequest) {
  const nonce = Buffer.from(crypto.randomUUID()).toString("base64");
  const isDev = process.env.NODE_ENV === "development";

  // Auth redirect: when auth is enabled and user has no session, redirect to /login
  if (authEnabled && !isPublicPath(request.nextUrl.pathname)) {
    const session = await auth();
    if (!session) {
      const loginUrl = new URL("/login", request.url);
      loginUrl.searchParams.set("callbackUrl", request.nextUrl.pathname);
      return NextResponse.redirect(loginUrl);
    }
  }

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", buildCsp(nonce, isDev));
  return response;
}

export function middleware(request: NextRequest) {
  return handleMiddleware(request);
}

export const config = {
  matcher: [
    // Match all paths except static files and Next.js internals
    "/((?!_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico)$).*)",
  ],
};
