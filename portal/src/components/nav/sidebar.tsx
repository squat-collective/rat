"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ChevronsLeft, ChevronsRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { NAV_ITEMS } from "@/lib/navigation";
import { ThemeToggle } from "@/components/theme-toggle";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { RAT_LOGO } from "@/lib/constants";

interface SidebarProps {
  collapsed: boolean;
  onToggle: () => void;
}

export function Sidebar({ collapsed, onToggle }: SidebarProps) {
  const pathname = usePathname();
  return (
    <TooltipProvider delayDuration={0}>
      <aside
        className={cn(
          "flex h-full flex-col border-r border-border/50 bg-card/80 backdrop-blur-sm transition-all duration-200",
          collapsed ? "w-14" : "w-56",
        )}
      >
        {/* Logo */}
        <div className="border-b border-border/50 px-3 py-2 overflow-hidden">
          {collapsed ? (
            <div className="flex items-center justify-center py-1 select-none">
              <span className="text-lg font-bold text-primary neon-text">R</span>
            </div>
          ) : (
            <pre
              className="rat-logo-hover text-[4.5px] leading-[5px] text-primary neon-text font-bold select-none whitespace-pre"
              data-text={RAT_LOGO}
            >
              {RAT_LOGO}
            </pre>
          )}
        </div>

        {/* Nav */}
        <nav className="flex-1 space-y-0.5 p-2">
          {NAV_ITEMS.map((item) => {
            const Icon = item.icon;
            const isActive =
              item.href === "/"
                ? pathname === "/"
                : pathname.startsWith(item.href);

            const link = (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "group flex items-center gap-3 px-3 py-2 text-xs font-medium tracking-wider transition-all",
                  collapsed && "justify-center px-0",
                  isActive
                    ? "bg-primary/10 text-primary border-l-2 border-primary neon-text"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/50 border-l-2 border-transparent",
                )}
              >
                <Icon
                  className={cn(
                    "h-3.5 w-3.5 shrink-0 transition-colors",
                    isActive
                      ? "text-primary"
                      : "text-muted-foreground group-hover:text-foreground",
                  )}
                />
                {!collapsed && (
                  <>
                    <span>{item.label}</span>
                    {isActive && (
                      <span className="ml-auto text-primary animate-pulse-neon">
                        _
                      </span>
                    )}
                  </>
                )}
              </Link>
            );

            if (collapsed) {
              return (
                <Tooltip key={item.href}>
                  <TooltipTrigger asChild>{link}</TooltipTrigger>
                  <TooltipContent side="right">
                    {item.label}
                  </TooltipContent>
                </Tooltip>
              );
            }

            return link;
          })}
        </nav>

        {/* Theme toggle */}
        <div className="border-t border-border/50 px-3 py-2">
          <ThemeToggle collapsed={collapsed} />
        </div>

        {/* Collapse toggle */}
        <div className="border-t border-border/50 px-3 py-2 flex justify-center">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onClick={onToggle}
                className="h-7 w-7 text-muted-foreground hover:text-foreground"
                aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
              >
                {collapsed ? (
                  <ChevronsRight className="h-4 w-4" aria-hidden="true" />
                ) : (
                  <ChevronsLeft className="h-4 w-4" aria-hidden="true" />
                )}
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">
              {collapsed ? "Expand sidebar" : "Collapse sidebar"}
            </TooltipContent>
          </Tooltip>
        </div>

      </aside>
    </TooltipProvider>
  );
}
