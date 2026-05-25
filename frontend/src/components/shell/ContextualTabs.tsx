import { NavLink, useLocation, useResolvedPath } from "react-router-dom";
import { cn } from "@/lib/cn";

export interface ContextualTab {
  to: string;
  label: string;
}

interface ContextualTabsProps {
  tabs: ContextualTab[];
  /**
   * Strict pathname match (end=true) so the index tab doesn't stay active
   * on child routes.
   */
  end?: boolean;
}

/**
 * Settings sub-route tabs. Custom isActive because tabs use absolute paths
 * and run-detail needed strict end matching so the index tab didn't stick active.
 */
export function ContextualTabs({ tabs, end = true }: ContextualTabsProps) {
  return (
    <nav
      aria-label="Run sections"
      className="inline-flex items-center gap-0.5 rounded-pill bg-line/70 p-1"
    >
      {tabs.map((tab) => (
        <Tab key={tab.to} to={tab.to} label={tab.label} end={end} />
      ))}
    </nav>
  );
}

function Tab({ to, label, end }: { to: string; label: string; end: boolean }) {
  const resolved = useResolvedPath(to);
  const loc = useLocation();
  // NavLink isActive is path-relative; tabs here use absolute paths.
  const isActive = end
    ? loc.pathname === resolved.pathname
    : loc.pathname === resolved.pathname || loc.pathname.startsWith(`${resolved.pathname}/`);

  return (
    <NavLink
      to={to}
      end={end}
      className={cn(
        "inline-flex h-7 items-center justify-center rounded-pill px-3 text-[13px] font-medium tracking-nav transition-colors duration-150 ease-out-soft",
        isActive
          ? "bg-surface text-ink shadow-[0_1px_2px_rgba(39,34,34,0.06)]"
          : "text-ink-nav hover:text-ink",
      )}
    >
      {label}
    </NavLink>
  );
}
