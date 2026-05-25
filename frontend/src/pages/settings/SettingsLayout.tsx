import { Outlet } from "react-router-dom";
import { ContextualTabs, type ContextualTab } from "@/components/shell/ContextualTabs";

/** Settings shell with sub-nav tabs; layout mirrors run-detail's tab pattern. */
const SETTINGS_TABS: ContextualTab[] = [{ to: "/settings/fleet", label: "Fleet Management" }];

export function SettingsLayout() {
  return (
    <div className="mx-auto max-w-container-content page-x py-8">
      <header className="mb-6 flex flex-col gap-3">
        <h1 className="text-section-sm font-semibold tracking-section text-ink md:text-section">
          Settings
        </h1>
        <p className="text-[12.5px] leading-5 text-ink-muted">
          Dev tooling for the demo — not operator-facing. Real executors and orchestrators are not
          provisioned through here.
        </p>
        <ContextualTabs tabs={SETTINGS_TABS} end={false} />
      </header>
      <Outlet />
    </div>
  );
}
