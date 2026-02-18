"use client";

import { useFeatures } from "@/hooks/use-api";
import { AlertTriangle, XCircle } from "lucide-react";

export function LicenseBanner() {
  const { data: features } = useFeatures();
  const license = features?.license;

  if (!license) return null;

  // Expired or invalid license
  if (!license.valid) {
    return (
      <div className="flex items-center gap-2 bg-destructive/20 border border-destructive/50 px-4 py-2 text-xs text-destructive">
        <XCircle className="h-3.5 w-3.5 shrink-0" />
        <span className="font-medium tracking-wider">
          License invalid: {license.error || "unknown error"}
        </span>
      </div>
    );
  }

  // Expiring within 30 days
  if (license.expires_at) {
    const expiresAt = new Date(license.expires_at);
    const daysLeft = Math.ceil(
      (expiresAt.getTime() - Date.now()) / (1000 * 60 * 60 * 24)
    );
    if (daysLeft <= 30) {
      return (
        <div className="flex items-center gap-2 bg-yellow-500/10 border border-yellow-500/30 px-4 py-2 text-xs text-yellow-400">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span className="font-medium tracking-wider">
            License expires in {daysLeft} day{daysLeft !== 1 ? "s" : ""}
          </span>
        </div>
      );
    }
  }

  return null;
}
