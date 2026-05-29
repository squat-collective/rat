import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  // The SDK is consumed via a `file:../sdk-typescript` link and lives outside
  // the portal dir. Next 16 builds with Turbopack by default, which only traces
  // modules under the inferred project root — point that root one level up so
  // the linked workspace package resolves (matches the Makefile mounts under
  // /workspace).
  turbopack: {
    root: join(__dirname, ".."),
  },
  experimental: {
    serverActions: {
      bodySizeLimit: "10mb",
    },
  },
  async headers() {
    return [
      {
        // Apply security headers to all routes
        // NOTE: CSP is set in middleware.ts (nonce-based, per-request)
        source: "/(.*)",
        headers: [
          {
            // Prevent MIME-type sniffing
            key: "X-Content-Type-Options",
            value: "nosniff",
          },
          {
            // Clickjacking protection (older browsers; CSP frame-ancestors is the modern version)
            key: "X-Frame-Options",
            value: "DENY",
          },
          {
            // Disable XSS auditor — modern browsers removed it; CSP is the real protection
            key: "X-XSS-Protection",
            value: "0",
          },
          {
            // Control referrer information sent with requests
            key: "Referrer-Policy",
            value: "strict-origin-when-cross-origin",
          },
          {
            // Restrict access to browser features the portal does not need
            key: "Permissions-Policy",
            value: "camera=(), microphone=(), geolocation=()",
          },
        ],
      },
    ];
  },
};

export default nextConfig;
