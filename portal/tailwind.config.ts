import type { Config } from "tailwindcss";
import tailwindcssAnimate from "tailwindcss-animate";
import tailwindcssTypography from "@tailwindcss/typography";

const config: Config = {
  darkMode: ["class"],
  content: ["./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        mono: [
          "JetBrains Mono",
          "Fira Code",
          "SF Mono",
          "Consolas",
          "monospace",
        ],
      },
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        neon: "hsl(var(--neon))",
        "neon-alt": "hsl(var(--neon-alt))",
        "neon-cyan": "hsl(var(--neon-cyan))",
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      keyframes: {
        "glitch-x": {
          "0%, 100%": { transform: "translateX(0)" },
          "10%": { transform: "translateX(-2px)" },
          "20%": { transform: "translateX(2px)" },
          "30%": { transform: "translateX(-1px)" },
          "40%": { transform: "translateX(1px)" },
          "50%": { transform: "translateX(0)" },
        },
        "pulse-neon": {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.7" },
        },
        "scanline-move": {
          "0%": { top: "0%" },
          "100%": { top: "100%" },
        },
        "dialog-glitch-in": {
          "0%": {
            opacity: "0",
            transform: "translate(-50%, -50%) scaleY(0.01) scaleX(0.4)",
            filter: "hue-rotate(90deg) brightness(3)",
          },
          "15%": {
            opacity: "1",
            transform: "translate(-50%, -50%) scaleY(0.6) scaleX(1.02) skewX(3deg)",
            filter: "hue-rotate(45deg) brightness(1.5)",
          },
          "30%": {
            transform: "translate(-50%, -50%) scaleY(1.05) scaleX(0.98) skewX(-2deg)",
            filter: "hue-rotate(-20deg) brightness(1.2)",
          },
          "50%": {
            transform: "translate(-50%, -50%) scaleY(0.97) scaleX(1.01) skewX(1deg)",
            filter: "hue-rotate(10deg) brightness(1.1)",
          },
          "70%": {
            transform: "translate(-50%, -50%) scaleY(1.01) skewX(-0.5deg)",
            filter: "none",
          },
          "100%": {
            transform: "translate(-50%, -50%) scale(1)",
            filter: "none",
          },
        },
        "dialog-glitch-out": {
          "0%": {
            opacity: "1",
            transform: "translate(-50%, -50%) scale(1)",
            filter: "none",
          },
          "20%": {
            transform: "translate(-50%, -50%) scaleX(1.03) skewX(2deg)",
            filter: "hue-rotate(30deg) brightness(1.3)",
          },
          "50%": {
            opacity: "0.7",
            transform: "translate(-50%, -50%) scaleY(0.5) scaleX(1.1) skewX(-3deg)",
            filter: "hue-rotate(90deg) brightness(2)",
          },
          "100%": {
            opacity: "0",
            transform: "translate(-50%, -50%) scaleY(0.01) scaleX(0.3)",
            filter: "hue-rotate(180deg) brightness(3)",
          },
        },
        "overlay-glitch-in": {
          "0%": { opacity: "0" },
          "10%": { opacity: "0.6" },
          "20%": { opacity: "0.3" },
          "40%": { opacity: "0.8" },
          "100%": { opacity: "1" },
        },
        "overlay-glitch-out": {
          "0%": { opacity: "1" },
          "60%": { opacity: "0.8" },
          "80%": { opacity: "0.3" },
          "90%": { opacity: "0.6" },
          "100%": { opacity: "0" },
        },
      },
      animation: {
        "glitch-x": "glitch-x 3s ease-in-out infinite",
        "pulse-neon": "pulse-neon 2s ease-in-out infinite",
        "scanline-move": "scanline-move 8s linear infinite",
        "dialog-glitch-in": "dialog-glitch-in 0.4s ease-out forwards",
        "dialog-glitch-out": "dialog-glitch-out 0.25s ease-in forwards",
        "overlay-glitch-in": "overlay-glitch-in 0.3s ease-out forwards",
        "overlay-glitch-out": "overlay-glitch-out 0.2s ease-in forwards",
      },
    },
  },
  plugins: [tailwindcssAnimate, tailwindcssTypography],
};

export default config;
