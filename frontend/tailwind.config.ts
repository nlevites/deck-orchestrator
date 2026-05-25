import type { Config } from "tailwindcss";

/**
 * Design tokens for the deck-fleet operator console.
 *
 * Color palette + typography are derived from medra.ai's public CSS via the
 * reference_ui_claude reference build; provenance in
 * reference_ui_claude/DESIGN_NOTES.md.
 *
 * Status colors map every value in deck-fleet/STATE_MACHINE.md to a visual.
 * If the state machine grows, update colors.status here AND
 * src/components/primitives/StatusPill.tsx together so the design system
 * stays exhaustive.
 */
const config: Config = {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        surface: {
          DEFAULT: "#ffffff",
          subtle: "#fafafa",
          warm: "#f7f6f6",
          footer: "#ece9e9",
          dark: "#494949",
          ink: "#272222",
        },
        ink: {
          DEFAULT: "#272222",
          muted: "#6c6161",
          sub: "#6b6b6a",
          button: "#737373",
          mobile: "#171717",
          nav: "#666666",
        },
        line: {
          DEFAULT: "#f1f1f1",
          strong: "#e4e4e4",
        },
        accent: {
          gold: "#b38849",
          link: "#1263c9",
          linkAlt: "#0082f3",
        },
        status: {
          // DFO state machine; keep in lockstep with StatusPill.tsx.
          pending: "#737373",
          ready: "#b38849",
          dispatched: "#1263c9",
          running: "#0082f3",
          completed: "#2f7d4d",
          failed: "#c2452f",
          ambiguous: "#a25a16",
          cancelled: "#737373",
          healthy: "#2f7d4d",
          busy: "#0082f3",
          unreachable: "#c2452f",
          recovering: "#a25a16",
          stale: "#a25a16",
        },
      },
      fontFamily: {
        sans: ['"Geist"', "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ['"Geist Mono"', "ui-monospace", "SFMono-Regular", "monospace"],
      },
      fontSize: {
        hero: ["72px", { lineHeight: "1.05", letterSpacing: "-0.04em" }],
        "hero-md": ["56px", { lineHeight: "1.05", letterSpacing: "-0.04em" }],
        "hero-sm": ["48px", { lineHeight: "1.05", letterSpacing: "-0.035em" }],
        section: ["45px", { lineHeight: "1.1", letterSpacing: "-0.02em" }],
        "section-sm": ["32px", { lineHeight: "1.15", letterSpacing: "-0.02em" }],
        eyebrow: ["12px", { lineHeight: "1.2", letterSpacing: "0.12em" }],
        protocol: ["20px", { lineHeight: "1.4", letterSpacing: "-0.02em" }],
      },
      letterSpacing: {
        hero: "-0.04em",
        section: "-0.02em",
        sub: "-0.015em",
        nav: "-0.01em",
      },
      borderRadius: {
        pill: "50px",
        card: "18px",
        panel: "20px",
      },
      spacing: {
        "page-x": "64px",
        "page-x-md": "24px",
        "page-x-sm": "18px",
      },
      maxWidth: {
        "container-nav": "1230px",
        "container-content": "1360px",
        "container-narrow": "990px",
        "container-small": "750px",
      },
      backdropBlur: {
        nav: "16px",
        chip: "10px",
        cta: "5px",
      },
      boxShadow: {
        nav: "0 1px 2px rgba(0,0,0,0.04), 0 8px 32px rgba(0,0,0,0.06)",
        card: "0 1px 2px rgba(39, 34, 34, 0.04), 0 4px 14px rgba(39, 34, 34, 0.05)",
        "card-hover": "0 2px 4px rgba(39, 34, 34, 0.06), 0 12px 28px rgba(39, 34, 34, 0.10)",
      },
      transitionTimingFunction: {
        "out-soft": "cubic-bezier(0.22, 1, 0.36, 1)",
      },
      keyframes: {
        "marquee-x": {
          "0%": { transform: "translateX(0%)" },
          "100%": { transform: "translateX(-50%)" },
        },
        "fade-up": {
          "0%": { opacity: "0", transform: "translateY(8px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
        pulse: {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.45" },
        },
      },
      animation: {
        marquee: "marquee-x 60s linear infinite",
        "marquee-slow": "marquee-x 120s linear infinite",
        "fade-up": "fade-up 0.6s cubic-bezier(0.22, 1, 0.36, 1) both",
        "pulse-slow": "pulse 2.4s cubic-bezier(0.22, 1, 0.36, 1) infinite",
      },
    },
  },
  plugins: [],
};

export default config;
