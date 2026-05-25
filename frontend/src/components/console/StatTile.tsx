import type { ComponentProps, ReactNode } from "react";
import { Card } from "@/components/primitives/Card";
import { StatusPill } from "@/components/primitives/StatusPill";

interface StatTileProps {
  label: string;
  value: ReactNode;
  status?: ComponentProps<typeof StatusPill>["status"];
  hint?: string;
}

export function StatTile({ label, value, status, hint }: StatTileProps) {
  return (
    <Card className="p-4">
      <div className="flex items-center justify-between gap-2">
        <span className="text-eyebrow font-mono uppercase text-ink-sub">{label}</span>
        {status && <StatusPill status={status} dot={false} />}
      </div>
      <div className="mt-2 font-mono text-[28px] font-semibold tracking-tight leading-none text-ink">
        {value}
      </div>
      {hint && (
        <div className="mt-1.5 text-[12px] leading-4 tracking-nav text-ink-muted">{hint}</div>
      )}
    </Card>
  );
}
