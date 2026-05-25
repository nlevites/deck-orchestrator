import { useEffect, useState, type HTMLAttributes } from "react";
import { relativeAge } from "@/lib/format";

interface TimeAgoProps extends Omit<HTMLAttributes<HTMLSpanElement>, "children"> {
  timestamp: string;
  /**
   * Tick interval in ms. Default 1000. Pass a larger value to throttle
   * the tick for far-past timestamps where second-by-second updates
   * are operationally meaningless.
   */
  intervalMs?: number;
}

/**
 * Live-ticking relative age. Use only where liveness matters — static lists
 * should call `relativeAge()` directly to avoid pointless intervals.
 */
export function TimeAgo({ timestamp, intervalMs = 1000, ...rest }: TimeAgoProps) {
  const [, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return <span {...rest}>{relativeAge(timestamp)}</span>;
}
