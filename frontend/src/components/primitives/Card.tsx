import { forwardRef, type HTMLAttributes } from "react";
import { cn } from "@/lib/cn";

type Surface = "white" | "warm" | "subtle";

interface CardProps extends HTMLAttributes<HTMLDivElement> {
  surface?: Surface;
  interactive?: boolean;
}

const surfaceStyles: Record<Surface, string> = {
  white: "bg-surface",
  warm: "bg-surface-warm",
  subtle: "bg-surface-subtle",
};

export const Card = forwardRef<HTMLDivElement, CardProps>(
  ({ className, surface = "white", interactive = false, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={cn(
          "rounded-panel border border-line",
          surfaceStyles[surface],
          interactive &&
            "transition-shadow duration-200 ease-out-soft hover:shadow-card-hover hover:-translate-y-px",
          className,
        )}
        {...props}
      />
    );
  },
);
Card.displayName = "Card";
