import type { Metadata } from "next";
import { Shield, CheckCircle, XCircle, AlertTriangle, Trash2, KeyRound, User, Puzzle } from "lucide-react";
import Link from "next/link";
import { serverApi, type FeaturesResponse } from "@/lib/server-api";
import { auth, authEnabled } from "@/lib/auth/server";
import { PluginSlot } from "@/components/plugins";

export const metadata: Metadata = {
  title: "Settings | RAT",
  description: "Platform settings, plugins, and license management",
};

export default async function SettingsPage() {
  let features: FeaturesResponse | null = null;
  try {
    features = await serverApi.features();
  } catch {
    // API unreachable — show defaults
  }

  const edition = features?.edition ?? "community";
  const license = features?.license;

  // Get auth session when auth is enabled
  const session = authEnabled ? await auth() : null;

  return (
    <div className="space-y-6 max-w-2xl">
      <h1 className="text-lg font-bold tracking-wider gradient-text">
        Settings
      </h1>

      <PluginSlot name="settings-nav" />

      {/* Edition info */}
      <div className="brutal-card p-4 space-y-2">
        <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
          Edition
        </h2>
        <p className="text-sm font-medium">
          RAT{" "}
          <span className="text-primary neon-text">{edition}</span>
        </p>
      </div>

      {/* License card */}
      {license ? (
        <div className="brutal-card p-4 space-y-3">
          <div className="flex items-center gap-2">
            <Shield className="h-4 w-4 text-primary" />
            <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
              License
            </h2>
          </div>

          <div className="grid grid-cols-2 gap-y-2 gap-x-4 text-xs">
            <span className="text-muted-foreground">Status</span>
            <span className="flex items-center gap-1.5">
              {license.valid ? (
                <>
                  <CheckCircle className="h-3 w-3 text-primary" />
                  <span className="text-primary">Valid</span>
                </>
              ) : (
                <>
                  <XCircle className="h-3 w-3 text-destructive" />
                  <span className="text-destructive">
                    {license.error || "Invalid"}
                  </span>
                </>
              )}
            </span>

            {license.tier && (
              <>
                <span className="text-muted-foreground">Tier</span>
                <span>{license.tier}</span>
              </>
            )}

            {license.org_id && (
              <>
                <span className="text-muted-foreground">Organization</span>
                <span>{license.org_id}</span>
              </>
            )}

            {license.seat_limit !== undefined && license.seat_limit > 0 && (
              <>
                <span className="text-muted-foreground">Seat Limit</span>
                <span>{license.seat_limit}</span>
              </>
            )}

            {license.expires_at && (
              <>
                <span className="text-muted-foreground">Expires</span>
                <LicenseExpiry expiresAt={license.expires_at} />
              </>
            )}
          </div>
        </div>
      ) : (
        <div className="brutal-card p-4 space-y-2">
          <div className="flex items-center gap-2">
            <Shield className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
              License
            </h2>
          </div>
          <p className="text-xs text-muted-foreground">
            No license key configured. Running community edition.
          </p>
        </div>
      )}

      {/* Data Retention */}
      <Link href="/settings/retention" className="block">
        <div className="brutal-card p-4 space-y-2 hover:border-primary/50 transition-colors cursor-pointer">
          <div className="flex items-center gap-2">
            <Trash2 className="h-4 w-4 text-primary" />
            <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
              Data Retention
            </h2>
          </div>
          <p className="text-xs text-muted-foreground">
            Configure automatic cleanup of old runs, logs, orphan branches, and
            Iceberg snapshots.
          </p>
        </div>
      </Link>

      {/* Plugin Management */}
      <Link href="/settings/plugins" className="block">
        <div className="brutal-card p-4 space-y-2 hover:border-primary/50 transition-colors cursor-pointer">
          <div className="flex items-center gap-2">
            <Puzzle className="h-4 w-4 text-primary" />
            <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
              Plugin Management
            </h2>
          </div>
          <p className="text-xs text-muted-foreground">
            View runner plugins installed in the pipeline execution container.
          </p>
        </div>
      </Link>

      {/* Authentication card — only when multi-user mode is active */}
      {features?.multi_user && (
        <div className="brutal-card p-4 space-y-3">
          <div className="flex items-center gap-2">
            <KeyRound className="h-4 w-4 text-primary" />
            <h2 className="text-xs font-bold tracking-wider text-muted-foreground">
              Authentication
            </h2>
          </div>
          <div className="grid grid-cols-2 gap-y-2 gap-x-4 text-xs">
            <span className="text-muted-foreground">Provider</span>
            <span>Keycloak</span>

            {process.env.KEYCLOAK_ISSUER && (
              <>
                <span className="text-muted-foreground">Issuer</span>
                <span className="truncate font-mono text-[10px]">
                  {process.env.KEYCLOAK_ISSUER}
                </span>
              </>
            )}

            <span className="text-muted-foreground">Status</span>
            <span className="flex items-center gap-1.5">
              {session?.user ? (
                <>
                  <User className="h-3 w-3 text-primary" />
                  <span className="text-primary">Authenticated</span>
                </>
              ) : (
                <>
                  <XCircle className="h-3 w-3 text-muted-foreground" />
                  <span className="text-muted-foreground">Not authenticated</span>
                </>
              )}
            </span>

            {session?.user?.email && (
              <>
                <span className="text-muted-foreground">User</span>
                <span>{session.user.email}</span>
              </>
            )}
          </div>
        </div>
      )}

      <PluginSlot name="settings-sections" />
    </div>
  );
}

function LicenseExpiry({ expiresAt }: { expiresAt: string }) {
  const d = new Date(expiresAt);
  const daysLeft = Math.ceil(
    (d.getTime() - Date.now()) / (1000 * 60 * 60 * 24),
  );
  return (
    <span className="flex items-center gap-1.5">
      {d.toLocaleDateString()}
      {daysLeft <= 30 && daysLeft > 0 && (
        <AlertTriangle className="h-3 w-3 text-yellow-400" />
      )}
    </span>
  );
}
