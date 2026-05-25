import { Link, Outlet } from "react-router-dom";
import { Breadcrumbs } from "./Breadcrumbs";
import { ConnectionBanner } from "./ConnectionBanner";
import { Sidebar } from "./Sidebar";
import { StepM } from "@/icons/StepM";
import { useLiveState } from "@/lib/live";

/**
 * Outer console chrome. Breadcrumbs are route-driven; run-detail tabs
 * collapsed into a single page.
 */
export function AppShell() {
  useLiveState();

  return (
    <div className="flex min-h-screen flex-col bg-surface-subtle text-ink">
      <ConnectionBanner />

      <header className="border-b border-line bg-surface">
        <div className="flex items-center py-2 pr-4">
          <Link
            to="/fleet"
            aria-label="Deck Fleet home"
            className="inline-flex h-10 w-14 shrink-0 items-center justify-center text-ink transition-opacity duration-150 ease-out-soft hover:opacity-80"
          >
            <StepM />
          </Link>
          <span className="h-5 w-px bg-line-strong" aria-hidden />
          <div className="flex min-w-0 items-center pl-3">
            <Breadcrumbs />
          </div>
        </div>
      </header>

      <div className="flex flex-1">
        <Sidebar />
        <main className="min-w-0 flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
