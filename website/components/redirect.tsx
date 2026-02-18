import { useEffect } from "react";
import { useRouter } from "next/router";

export function Redirect({ to }: { to: string }) {
  const router = useRouter();

  useEffect(() => {
    router.replace(to);
  }, [to, router]);

  return (
    <p>
      Redirecting to <a href={to}>{to}</a>...
    </p>
  );
}

export function NotFoundRedirect() {
  const router = useRouter();
  const path = router.asPath.split("?")[0].split("#")[0];

  const prefixMap: Record<string, string> = {
    "/tutorial": "/getting-started/tutorial",
    "/architecture": "/contributing/architecture",
    "/deployment": "/self-hosting",
    "/data-best-practices": "/guides/best-practices",
  };

  useEffect(() => {
    for (const [oldPrefix, newPrefix] of Object.entries(prefixMap)) {
      if (path === oldPrefix || path.startsWith(oldPrefix + "/")) {
        router.replace(path.replace(oldPrefix, newPrefix));
        return;
      }
    }
  }, [path, router]);

  return null;
}
