import { forwardRef, type AnchorHTMLAttributes } from "react";
import { cn } from "@/lib/cn";

interface PillButtonProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  /**
   * Tone matches Medra's CTA variants:
   *  - `tertiary`  →  bg-line-strong / text-ink-button (default nav CTA)
   *  - `inverse`   →  bg-ink / text-white (used on dark surfaces)
   *  - `frosted`   →  bg-white/30 / text-white / backdrop-blur (used over imagery)
   */
  tone?: "tertiary" | "inverse" | "frosted";
}

const toneStyles: Record<NonNullable<PillButtonProps["tone"]>, string> = {
  tertiary: "bg-line-strong text-ink-button hover:text-ink hover:bg-[#dcdcdc]",
  inverse: "bg-surface-ink text-white hover:bg-black",
  frosted: "bg-white/30 text-white backdrop-blur-cta hover:bg-white/45",
};

export const PillButton = forwardRef<HTMLAnchorElement, PillButtonProps>(
  ({ className, tone = "tertiary", ...props }, ref) => {
    return (
      <a
        ref={ref}
        className={cn(
          "inline-flex items-center justify-center rounded-pill px-3 py-[10px] text-[14px] font-medium tracking-nav transition-colors duration-150 ease-out-soft",
          toneStyles[tone],
          className,
        )}
        {...props}
      />
    );
  },
);
PillButton.displayName = "PillButton";
