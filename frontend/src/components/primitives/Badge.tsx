import { forwardRef, type HTMLAttributes } from "react";
import { cn } from "@/lib/cn";

type Tone = "neutral" | "muted" | "warm" | "cool";

interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: Tone;
}

const toneStyles: Record<Tone, string> = {
  neutral: "bg-surface-warm text-ink",
  muted: "bg-line text-ink-nav",
  warm: "bg-[#f4ece1] text-accent-gold",
  cool: "bg-[#e7eef9] text-accent-link",
};

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(
  ({ className, tone = "neutral", ...props }, ref) => {
    return (
      <span
        ref={ref}
        className={cn(
          "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium tracking-nav",
          toneStyles[tone],
          className,
        )}
        {...props}
      />
    );
  },
);
Badge.displayName = "Badge";
