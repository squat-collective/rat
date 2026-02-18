/** Shared theme constants — single source of truth for layer/status styling. */

/** RAT ASCII art logo — used in sidebar and home page. */
export const RAT_LOGO = `  ██▀███   ▄▄▄      ▄▄▄█████▓
 ▓██ ▒ ██▒▒████▄    ▓  ██▒ ▓▒
 ▓██ ░▄█ ▒▒██  ▀█▄  ▒ ▓██░ ▒░
 ▒██▀▀█▄  ░██▄▄▄▄██ ░ ▓██▓ ░
 ░██▓ ▒██▒ ▓█   ▓██▒  ▒██▒ ░
 ░ ▒▓ ░▒▓░ ▒▒   ▓▒█░  ▒ ░░
   ░▒ ░ ▒░  ▒   ▒▒ ░    ░
   ░░   ░   ░   ▒     ░
    ░           ░  ░`;

/** Layer badge colors (background + text + border) for Badge components. */
export const LAYER_BADGE_COLORS: Record<string, string> = {
  bronze: "bg-orange-900/30 text-orange-400 border-orange-700",
  silver: "bg-zinc-800/30 text-zinc-300 border-zinc-600",
  gold: "bg-yellow-900/30 text-yellow-400 border-yellow-700",
};

/** Run status badge colors (background + text + border). */
export const STATUS_COLORS: Record<string, string> = {
  success: "bg-primary/20 text-primary border-primary/50",
  failed: "bg-destructive/20 text-destructive border-destructive/50",
  running: "bg-neon-cyan/20 text-neon-cyan border-neon-cyan/50",
  pending: "bg-muted text-muted-foreground border-muted",
  cancelled: "bg-muted text-muted-foreground border-muted",
};

/** Run status emoji indicators. */
export const STATUS_EMOJI: Record<string, string> = {
  success: "\u2705",
  failed: "\u274C",
  running: "\u23F3",
  pending: "\u23F8",
  cancelled: "\u26D4",
};
