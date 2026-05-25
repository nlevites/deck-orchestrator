import { Navigate, Route, Routes, useParams } from "react-router-dom";
import { AppShell } from "@/components/shell/AppShell";
import { ConnectionProvider } from "@/lib/connection/ConnectionContext";
import { DesignSystemSmokePage } from "@/pages/_dev/DesignSystemSmokePage";
import { ConnectionDebugPage } from "@/pages/_dev/ConnectionDebugPage";
import { DeckDetailPage } from "@/pages/console/deck-detail/DeckDetailPage";
import { FleetDashboardPage } from "@/pages/console/FleetDashboardPage";
import { FleetGridPage } from "@/pages/console/FleetGridPage";
import { RunsListPage } from "@/pages/console/RunsListPage";
import { RunDetailPage } from "@/pages/console/run-detail/RunDetailPage";
import { SubmitRunPage } from "@/pages/console/SubmitRunPage";
import { SettingsLayout } from "@/pages/settings/SettingsLayout";
import { FleetManagementPage } from "@/pages/settings/FleetManagementPage";

// /settings/* is dev-only tooling for the demo (orchestrator restart,
// supervisor stop/restart, deck attach/detach/release). Real executors
// and orchestrators are not provisioned through the console; in a
// production build the route is hidden and any deep-link redirects to
// /fleet. Vite inlines this constant at build time.
const DEV_TOOLS_ENABLED = import.meta.env.DEV;

/**
 * Console router.
 *
 *   - Every console route is nested under <AppShell> so the top bar,
 *     sidebar, and connection banner are shared.
 *   - /runs/:id is a single page (no longer a layout route). The legacy
 *     /events sub-path stays as a deep-link that opens the activity
 *     drawer; /deck and /resolve redirect to the parent (their content
 *     is now inline on the parent page).
 *   - /_design renders the Phase 1 smoke page for visual regression.
 *   - /debug/* and /settings/* are dev-only surfaces, gated above.
 */
export function App() {
  return (
    <ConnectionProvider>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<Navigate to="/fleet" replace />} />
          <Route path="/fleet" element={<FleetDashboardPage />} />
          <Route path="/fleet/grid" element={<FleetGridPage />} />
          <Route path="/decks/:id" element={<DeckDetailPage />} />
          <Route path="/runs" element={<RunsListPage />} />

          <Route path="/runs/:id" element={<RunDetailPage />} />
          <Route path="/runs/:id/events" element={<RunDetailPage />} />
          <Route path="/runs/:id/deck" element={<RedirectToRun />} />
          <Route path="/runs/:id/resolve" element={<RedirectToRun />} />

          <Route path="/submit" element={<SubmitRunPage />} />

          {DEV_TOOLS_ENABLED ? (
            <Route path="/settings" element={<SettingsLayout />}>
              <Route index element={<Navigate to="/settings/fleet" replace />} />
              <Route path="fleet" element={<FleetManagementPage />} />
            </Route>
          ) : (
            <Route path="/settings/*" element={<Navigate to="/fleet" replace />} />
          )}

          {DEV_TOOLS_ENABLED && (
            <Route path="/debug/connection" element={<ConnectionDebugPage />} />
          )}
          <Route path="/_design" element={<DesignSystemSmokePage />} />
        </Route>

        <Route path="*" element={<Navigate to="/fleet" replace />} />
      </Routes>
    </ConnectionProvider>
  );
}

/**
 * Tiny helper for legacy /runs/:id/{deck,resolve} URLs — content now
 * lives on the parent page.
 */
function RedirectToRun() {
  const { id } = useParams<{ id: string }>();
  return <Navigate to={`/runs/${id ?? ""}`} replace />;
}
