import { forwardRef, type ButtonHTMLAttributes } from "react";
import { cn } from "@/lib/cn";

type Variant = "primary" | "secondary" | "ghost" | "link" | "danger";
type Size = "sm" | "md" | "lg";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
}

const variantStyles: Record<Variant, string> = {
  primary:
    "bg-surface-ink text-white hover:bg-black active:translate-y-px disabled:bg-line-strong disabled:text-ink-button",
  secondary: "bg-line-strong text-ink-button hover:text-ink hover:bg-[#dcdcdc]",
  ghost: "bg-transparent text-ink-nav hover:text-ink",
  link: "bg-transparent text-accent-link hover:text-accent-linkAlt underline-offset-4 hover:underline",
  danger: "bg-status-failed text-white hover:bg-[#a83a26] active:translate-y-px",
};

const sizeStyles: Record<Size, string> = {
  sm: "h-8 px-3 text-[13px]",
  md: "h-9 px-4 text-sm",
  lg: "h-11 px-5 text-[15px]",
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = "primary", size = "md", type = "button", ...props }, ref) => {
    return (
      <button
        ref={ref}
        type={type}
        className={cn(
          "inline-flex items-center justify-center gap-2 rounded-pill font-medium tracking-nav transition-colors duration-150 ease-out-soft disabled:cursor-not-allowed",
          variantStyles[variant],
          sizeStyles[size],
          className,
        )}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";
