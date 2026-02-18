import {
  Activity,
  Database,
  GitBranch,
  Home,
  Inbox,
  Play,
  Search,
  Settings,
} from "lucide-react";

export type NavItem = {
  label: string;
  href: string;
  icon: typeof Home;
};

export const NAV_ITEMS: NavItem[] = [
  { label: "Home", href: "/", icon: Home },
  { label: "Pipelines", href: "/pipelines", icon: Play },
  { label: "Query", href: "/query", icon: Search },
  { label: "Runs", href: "/runs", icon: Activity },
  { label: "Lineage", href: "/lineage", icon: GitBranch },
  { label: "Explorer", href: "/explorer", icon: Database },
  { label: "Landing", href: "/landing", icon: Inbox },
  { label: "Settings", href: "/settings", icon: Settings },
];
