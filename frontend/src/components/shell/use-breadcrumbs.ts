import { useLocation, matchPath } from "react-router-dom";

export interface Crumb {
  label: string;
  to?: string;
}

/**
 * Pathname → breadcrumb trail. Add a matcher to `RULES`; first match wins.
 *
 * Conventions: "Console" prefix is never a link; top-level sections are
 * flat siblings (Decks is not under Fleet); detail pages use raw URL ids.
 */
export function useBreadcrumbs(): Crumb[] {
  const { pathname } = useLocation();
  for (const rule of RULES) {
    const match = matchPath({ path: rule.path, end: rule.end ?? true }, pathname);
    if (match) return [CONSOLE_PREFIX, ...rule.build(match.params)];
  }
  return [CONSOLE_PREFIX];
}

const CONSOLE_PREFIX: Crumb = { label: "Console" };

interface Rule {
  path: string;
  end?: boolean;
  build: (params: Readonly<Record<string, string | undefined>>) => Crumb[];
}

/**
 * Order matters: more-specific routes first. The /events deep-link
 * (drawer) comes before /runs/:id so the trail extends into "Events".
 * /runs/:id/{deck,resolve} now redirect to /runs/:id (App.tsx); we
 * don't carry breadcrumb branches for them.
 */
const RULES: Rule[] = [
  {
    path: "/runs/:id/events",
    build: ({ id = "" }) => [
      { label: "Runs", to: "/runs" },
      { label: id, to: `/runs/${id}` },
      { label: "Events" },
    ],
  },
  {
    path: "/runs/:id",
    build: ({ id = "" }) => [{ label: "Runs", to: "/runs" }, { label: id }],
  },
  { path: "/runs", build: () => [{ label: "Runs" }] },

  {
    path: "/decks/:id",
    build: ({ id = "" }) => [{ label: "Decks", to: "/fleet/grid" }, { label: id }],
  },

  { path: "/fleet/grid", build: () => [{ label: "Decks" }] },
  { path: "/fleet", build: () => [{ label: "Fleet" }] },
  { path: "/submit", build: () => [{ label: "New run" }] },
  { path: "/settings", build: () => [{ label: "Settings" }] },

  { path: "/debug/connection", build: () => [{ label: "Debug" }, { label: "Connection" }] },
  { path: "/_design", build: () => [{ label: "Design system" }] },
];
