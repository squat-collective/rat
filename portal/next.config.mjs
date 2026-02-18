/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
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
