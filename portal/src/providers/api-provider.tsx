"use client";

import { RatClient } from "@squat-collective/rat-client";
import { createContext, useContext, useMemo } from "react";
import { useAuthSession } from "@/lib/auth/client";
import { SWRConfig } from "swr";
import { PUBLIC_API_URL } from "@/lib/api-client";

const ApiContext = createContext<RatClient | null>(null);

export function ApiProvider({ children }: { children: React.ReactNode }) {
  const { data: session } = useAuthSession();
  const accessToken = session?.accessToken;

  const client = useMemo(
    () =>
      new RatClient({
        apiUrl: PUBLIC_API_URL,
        // When an access token is present (pro auth), inject Bearer header
        ...(accessToken
          ? {
              onRequest: [
                (req) => {
                  req.headers["Authorization"] = `Bearer ${accessToken}`;
                },
              ],
            }
          : {}),
      }),
    [accessToken],
  );

  return (
    <SWRConfig value={{ onError: (err) => console.error("[SWR]", err) }}>
      <ApiContext.Provider value={client}>{children}</ApiContext.Provider>
    </SWRConfig>
  );
}

export function useApiClient(): RatClient {
  const ctx = useContext(ApiContext);
  if (!ctx) throw new Error("useApiClient must be inside ApiProvider");
  return ctx;
}
