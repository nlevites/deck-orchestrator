import { forwardRef, type HTMLAttributes } from "react";
import { cn } from "@/lib/cn";

export const KBD = forwardRef<HTMLElement, HTMLAttributes<HTMLElement>>(
  ({ className, ...props }, ref) => {
    return (
      <kbd
        ref={ref}
        className={cn(
          "inline-flex h-5 min-w-[20px] items-center justify-center rounded border border-line bg-surface px-1.5 font-mono text-[11px] font-medium text-ink-nav shadow-[0_1px_0_#e4e4e4]",
          className,
        )}
        {...props}
      />
    );
  },
);
KBD.displayName = "KBD";
