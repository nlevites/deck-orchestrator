import { Button } from "@/components/primitives/Button";
import { Card } from "@/components/primitives/Card";
import type { ConnectionState } from "@/lib/connection/connection-ctx";
import { useConnection } from "@/lib/connection/use-connection";
import { cn } from "@/lib/cn";

/**
 * Dev-only switch for the tri-state ConnectionBanner. Hitting one of the
 * buttons sets a session override so we can screenshot each banner state
 * without flipping the network or restarting the orchestrator.
 *
 * URL shortcut also works: append ?connection=offline|live|degraded|ok to
 * any route.
 */
const states: { state: ConnectionState; label: string; desc: string }[] = [
  {
    state: "OK",
    label: "OK",
    desc: "Banner is hidden — everything green.",
  },
  {
    state: "OFFLINE",
    label: "Offline",
    desc: "Browser navigator.onLine === false. Mutations disabled in the real flow.",
  },
  {
    state: "LIVE_PAUSED",
    label: "Live paused",
    desc: "No successful /api/state poll within ~2s. Orchestrator likely down or unreachable.",
  },
  {
    state: "DEGRADED_MODE",
    label: "Degraded mode",
    desc: "Orchestrator returns 503 during startup reconciliation. Reads OK, mutations refused.",
  },
];

export function ConnectionDebugPage() {
  const { state, override, setOverride } = useConnection();
  return (
    <div className="mx-auto max-w-container-content page-x py-10">
      <header className="flex flex-col gap-2">
        <span className="text-eyebrow font-mono uppercase text-ink-sub">/debug/connection</span>
        <h1 className="text-section-sm font-semibold tracking-section text-ink md:text-section">
          Connection banner debug
        </h1>
        <p className="max-w-container-small text-[15px] tracking-sub text-ink-muted">
          Flip the override to screenshot each banner state. Dev-only; not mounted in a production
          build.
        </p>
      </header>

      <Card className="mt-8 p-6">
        <div className="flex flex-wrap items-baseline justify-between gap-3">
          <div>
            <div className="text-eyebrow font-mono uppercase text-ink-sub">Current state</div>
            <div className="mt-1 font-mono text-[24px] font-semibold text-ink">{state}</div>
          </div>
          {override && (
            <Button variant="link" onClick={() => setOverride(null)}>
              Clear override
            </Button>
          )}
        </div>

        <div className="mt-6 grid grid-cols-1 gap-3 md:grid-cols-2">
          {states.map((row) => {
            const isActive = override === row.state;
            return (
              <button
                key={row.state}
                type="button"
                onClick={() => setOverride(row.state)}
                className={cn(
                  "rounded-card border p-4 text-left transition-colors duration-150 ease-out-soft",
                  isActive
                    ? "border-ink/40 bg-surface-warm"
                    : "border-line bg-surface hover:border-line-strong",
                )}
              >
                <div className="flex items-center justify-between">
                  <span className="text-[14px] font-semibold tracking-nav text-ink">
                    {row.label}
                  </span>
                  <span className="font-mono text-[11px] text-ink-sub">{row.state}</span>
                </div>
                <p className="mt-1.5 text-[13px] leading-5 text-ink-muted">{row.desc}</p>
              </button>
            );
          })}
        </div>
      </Card>
    </div>
  );
}
