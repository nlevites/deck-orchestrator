import { Badge } from "@/components/primitives/Badge";
import { Button } from "@/components/primitives/Button";
import { Card } from "@/components/primitives/Card";
import { KBD } from "@/components/primitives/KBD";
import { PillButton } from "@/components/primitives/PillButton";
import { StatusPill } from "@/components/primitives/StatusPill";
import {
  ALL_DECK_HEALTHS,
  ALL_DECK_JOB_STATUSES,
  ALL_RUN_STATUSES,
} from "@/components/primitives/status-helpers";
import { StepM } from "@/icons/StepM";

/** Dev smoke page: one instance of every design-system primitive. */
export function DesignSystemSmokePage() {
  return (
    <div className="min-h-screen bg-surface-subtle text-ink">
      <header className="border-b border-line bg-surface">
        <div className="mx-auto flex max-w-container-content items-center gap-3 page-x py-5">
          <StepM className="text-ink" />
          <div className="flex flex-col leading-tight">
            <span className="text-eyebrow font-mono uppercase text-ink-sub">
              Deck Fleet · phase 1
            </span>
            <span className="text-[15px] font-medium tracking-nav text-ink">
              Design system smoke test
            </span>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-container-content page-x py-10">
        <h1 className="text-section-sm font-semibold tracking-section text-ink md:text-section">
          Hello, DFO
        </h1>
        <p className="mt-3 max-w-container-small text-[15px] tracking-sub text-ink-muted">
          Geist sans + mono load from <span className="font-mono">@fontsource</span>. Tailwind
          tokens, primitives, and the exhaustive <span className="font-mono">StatusPill</span> are
          wired up. If any pill below loses its color or the body font drops to system, the design
          system is broken.
        </p>

        <section className="mt-10 grid grid-cols-1 gap-6 md:grid-cols-3">
          <SwatchPanel title="deck_job · status" subtitle="STATE_MACHINE.md §3.1">
            <div className="flex flex-wrap gap-2">
              {ALL_DECK_JOB_STATUSES.map((s) => (
                <StatusPill key={s} status={s} />
              ))}
            </div>
          </SwatchPanel>

          <SwatchPanel title="run · status (derived)" subtitle="STATE_MACHINE.md §4.1">
            <div className="flex flex-wrap gap-2">
              {ALL_RUN_STATUSES.map((s) => (
                <StatusPill key={s} status={s} />
              ))}
            </div>
          </SwatchPanel>

          <SwatchPanel title="deck · health" subtitle="STATE_MACHINE.md §5">
            <div className="flex flex-wrap gap-2">
              {ALL_DECK_HEALTHS.map((s) => (
                <StatusPill key={s} status={s} />
              ))}
            </div>
          </SwatchPanel>
        </section>

        <section className="mt-10 grid grid-cols-1 gap-6 md:grid-cols-2">
          <SwatchPanel title="Button variants">
            <div className="flex flex-wrap items-center gap-3">
              <Button variant="primary">Primary</Button>
              <Button variant="secondary">Secondary</Button>
              <Button variant="ghost">Ghost</Button>
              <Button variant="link">Link</Button>
              <Button variant="danger">Danger</Button>
              <Button variant="primary" disabled>
                Disabled
              </Button>
            </div>
            <div className="mt-4 flex flex-wrap items-center gap-3">
              <Button size="sm">sm</Button>
              <Button size="md">md</Button>
              <Button size="lg">lg</Button>
              <PillButton href="#" tone="tertiary">
                PillButton
              </PillButton>
            </div>
          </SwatchPanel>

          <SwatchPanel title="Badge tones">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="neutral">neutral</Badge>
              <Badge tone="muted">muted</Badge>
              <Badge tone="warm">warm</Badge>
              <Badge tone="cool">cool</Badge>
            </div>
            <div className="mt-5 flex flex-wrap items-center gap-2 text-[13px] text-ink-muted">
              <span>Resolve ambiguous job</span>
              <KBD>⌘</KBD>
              <KBD>⇧</KBD>
              <KBD>R</KBD>
            </div>
          </SwatchPanel>
        </section>

        <section className="mt-10">
          <SwatchPanel title="Card surfaces">
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <Card surface="white" className="p-5">
                <div className="text-eyebrow font-mono uppercase text-ink-sub">white</div>
                <div className="mt-2 text-[15px] font-medium tracking-nav">
                  Default panel surface
                </div>
              </Card>
              <Card surface="warm" className="p-5">
                <div className="text-eyebrow font-mono uppercase text-ink-sub">warm</div>
                <div className="mt-2 text-[15px] font-medium tracking-nav">
                  Warm content surface
                </div>
              </Card>
              <Card surface="subtle" interactive className="p-5">
                <div className="text-eyebrow font-mono uppercase text-ink-sub">
                  subtle + interactive
                </div>
                <div className="mt-2 text-[15px] font-medium tracking-nav">
                  Hover to feel the lift
                </div>
              </Card>
            </div>
          </SwatchPanel>
        </section>

        <footer className="mt-12 border-t border-line pt-6 text-[12px] tracking-sub text-ink-muted">
          <span className="font-mono">phase 1</span> · design system smoke test · remove this page
          once the real shell lands in phase 2
        </footer>
      </main>
    </div>
  );
}

interface SwatchPanelProps {
  title: string;
  subtitle?: string;
  children: React.ReactNode;
}

function SwatchPanel({ title, subtitle, children }: SwatchPanelProps) {
  return (
    <Card className="p-5">
      <div className="mb-4 flex items-baseline justify-between gap-3">
        <h2 className="text-[15px] font-semibold tracking-sub text-ink">{title}</h2>
        {subtitle && (
          <span className="text-eyebrow font-mono uppercase text-ink-sub">{subtitle}</span>
        )}
      </div>
      {children}
    </Card>
  );
}
