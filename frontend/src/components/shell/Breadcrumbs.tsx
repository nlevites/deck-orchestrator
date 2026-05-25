import { Fragment } from "react";
import { Link } from "react-router-dom";
import { ChevronRight } from "lucide-react";
import { cn } from "@/lib/cn";
import { useBreadcrumbs } from "./use-breadcrumbs";

interface BreadcrumbsProps {
  className?: string;
}

/**
 * Breadcrumb trail; last segment is current page (`aria-current="page"`).
 */
export function Breadcrumbs({ className }: BreadcrumbsProps) {
  const crumbs = useBreadcrumbs();
  return (
    <nav aria-label="Breadcrumb" className={cn("min-w-0", className)}>
      <ol className="flex flex-wrap items-center gap-1 text-[13px] tracking-nav">
        {crumbs.map((c, i) => {
          const isLast = i === crumbs.length - 1;
          return (
            <Fragment key={`${c.label}-${i}`}>
              <li className="flex min-w-0 items-center">
                {c.to && !isLast ? (
                  <Link
                    to={c.to}
                    className="truncate rounded px-1.5 py-0.5 text-ink-nav transition-colors duration-150 ease-out-soft hover:bg-line/60 hover:text-ink"
                  >
                    {c.label}
                  </Link>
                ) : (
                  <span
                    aria-current={isLast ? "page" : undefined}
                    className={cn(
                      "truncate px-1.5 py-0.5",
                      isLast ? "font-medium text-ink" : "text-ink-sub",
                    )}
                  >
                    {c.label}
                  </span>
                )}
              </li>
              {!isLast && (
                <li aria-hidden="true" className="text-ink-sub">
                  <ChevronRight size={12} strokeWidth={2} />
                </li>
              )}
            </Fragment>
          );
        })}
      </ol>
    </nav>
  );
}
