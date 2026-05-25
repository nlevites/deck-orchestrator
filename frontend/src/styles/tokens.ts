/**
 * Mirror of tailwind.config.ts tokens for non-Tailwind code paths
 * (inline SVG fills, dynamic styles where a class would be awkward).
 *
 * Update both this file and tailwind.config.ts together.
 */
export const colors = {
  surface: "#ffffff",
  surfaceSubtle: "#fafafa",
  surfaceWarm: "#f7f6f6",
  ink: "#272222",
  inkMuted: "#6c6161",
  inkSub: "#6b6b6a",
  inkNav: "#666666",
  line: "#f1f1f1",
  lineStrong: "#e4e4e4",
  accentGold: "#b38849",
  accentLink: "#1263c9",
  accentLinkAlt: "#0082f3",
  status: {
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
} as const;

export const radii = {
  pill: 50,
  card: 18,
  panel: 20,
} as const;
