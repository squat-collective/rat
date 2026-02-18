"use client";

import { useTheme } from "next-themes";
import { useEffect, useState } from "react";
import { Sun, Moon, Monitor } from "lucide-react";

interface ThemeToggleProps {
  collapsed?: boolean;
}

export function ThemeToggle({ collapsed }: ThemeToggleProps) {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);

  useEffect(() => setMounted(true), []);

  if (!mounted) return null;

  const modes = [
    { value: "dark", icon: Moon, label: "DARK" },
    { value: "light", icon: Sun, label: "LIGHT" },
    { value: "system", icon: Monitor, label: "SYS" },
  ] as const;

  return (
    <div className={`flex items-center gap-0.5 rounded border border-border bg-background/50 p-0.5 ${collapsed ? "flex-col" : ""}`}>
      {modes.map(({ value, icon: Icon, label }) => (
        <button
          key={value}
          onClick={() => setTheme(value)}
          className={`flex items-center gap-1 rounded px-2 py-1 text-[10px] font-bold tracking-wider transition-all ${
            theme === value
              ? "bg-primary text-primary-foreground neon-text"
              : "text-muted-foreground hover:text-foreground"
          }`}
          title={label}
          aria-label={`Switch to ${value} mode`}
        >
          <Icon className="h-3 w-3" />
          {!collapsed && <span className="hidden sm:inline">{label}</span>}
        </button>
      ))}
    </div>
  );
}
