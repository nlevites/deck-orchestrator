/**
 * `useEventFilter` — operator's enabled-family selection for the
 * console event tail. Persisted to `localStorage` so the choice
 * follows the operator across `/fleet`, `/runs/:id/events`, and
 * `/decks/:id` (and across browser tabs in the same origin via the
 * `storage` event).
 *
 * Default (missing key) is "all enabled". Malformed values fall back
 * to the default — the chip surface is a UX nicety; we never want a
 * corrupt key to crash the dashboard.
 *
 * Storage shape (intentionally tiny, no JSON, no version field):
 *   key:   `dfo:eventFilter`
 *   value: comma-separated list of enabled family ids, e.g.
 *          `"runs,jobs,resolutions,other"` (health hidden).
 *          Empty string = all hidden.
 *
 * Uses a custom `dfo:event-filter:changed` window event to push
 * in-tab updates to other `useEventFilter` consumers (the native
 * `storage` event only fires across tabs). Cross-tab updates land
 * via `storage` as usual.
 */
import { useCallback, useEffect, useState } from "react";
import { FAMILIES, isKnownFamily, type EventFamily } from "./families";

const STORAGE_KEY = "dfo:eventFilter";
const IN_TAB_EVENT = "dfo:event-filter:changed";

export interface UseEventFilterResult {
  active: ReadonlySet<EventFamily>;
  isAll: boolean;
  toggle: (family: EventFamily) => void;
  reset: () => void;
  setActive: (next: ReadonlySet<EventFamily>) => void;
}

export function useEventFilter(): UseEventFilterResult {
  const [active, setActiveState] = useState<ReadonlySet<EventFamily>>(() => readFromStorage());

  useEffect(() => {
    if (typeof window === "undefined") return undefined;

    const handleStorage = (e: StorageEvent) => {
      if (e.key !== STORAGE_KEY && e.key !== null) return;
      setActiveState(readFromStorage());
    };
    const handleInTab = () => {
      setActiveState(readFromStorage());
    };

    window.addEventListener("storage", handleStorage);
    window.addEventListener(IN_TAB_EVENT, handleInTab);
    return () => {
      window.removeEventListener("storage", handleStorage);
      window.removeEventListener(IN_TAB_EVENT, handleInTab);
    };
  }, []);

  const persist = useCallback((next: ReadonlySet<EventFamily>) => {
    setActiveState(next);
    writeToStorage(next);
  }, []);

  const toggle = useCallback(
    (family: EventFamily) => {
      // Pivot model — operator clicks chip "Jobs" while every family
      // is enabled (the resting state) expecting "Jobs only", not
      // "everything except Jobs". Plain Set toggling does the latter,
      // which is what the first cut shipped and what the user flagged.
      //
      //   all on  + click X    → {X} only
      //   subset  + click X    → toggle X's membership
      //   subset  + last off   → snap back to all on
      //   subset  + becomes all → snap back to all on (clean state)
      if (active.size === FAMILIES.length) {
        persist(new Set([family]));
        return;
      }
      const next = new Set(active);
      if (next.has(family)) {
        next.delete(family);
        if (next.size === 0) {
          persist(new Set(FAMILIES));
          return;
        }
      } else {
        next.add(family);
        if (next.size === FAMILIES.length) {
          persist(new Set(FAMILIES));
          return;
        }
      }
      persist(next);
    },
    [active, persist],
  );

  const reset = useCallback(() => {
    if (typeof window !== "undefined") {
      try {
        window.localStorage.removeItem(STORAGE_KEY);
        window.dispatchEvent(new Event(IN_TAB_EVENT));
      } catch {
        // localStorage may be unavailable (Safari private mode, quota,
        // etc). Drop the persistence and keep the in-memory state
        // consistent — chips still respond to clicks.
      }
    }
    setActiveState(new Set(FAMILIES));
  }, []);

  return {
    active,
    isAll: active.size === FAMILIES.length,
    toggle,
    reset,
    setActive: persist,
  };
}

function readFromStorage(): ReadonlySet<EventFamily> {
  if (typeof window === "undefined") return new Set(FAMILIES);
  let raw: string | null = null;
  try {
    raw = window.localStorage.getItem(STORAGE_KEY);
  } catch {
    return new Set(FAMILIES);
  }
  if (raw === null) return new Set(FAMILIES);
  // An empty string is a legal "all hidden" state distinct from
  // "missing key". We only fall back to the all-on default when the
  // key was never written.
  const parts = raw
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
  const out = new Set<EventFamily>();
  for (const p of parts) {
    if (isKnownFamily(p)) out.add(p);
  }
  // If the stored value contained nothing recognisable AND the key
  // wasn't an explicit empty string, treat as corrupt and reset.
  if (out.size === 0 && raw.trim().length > 0) return new Set(FAMILIES);
  return out;
}

function writeToStorage(active: ReadonlySet<EventFamily>): void {
  if (typeof window === "undefined") return;
  // Serialise in canonical FAMILIES order so the persisted string is
  // stable across writes — easier to read in devtools and easier to
  // diff in tests.
  const csv = FAMILIES.filter((f) => active.has(f)).join(",");
  try {
    window.localStorage.setItem(STORAGE_KEY, csv);
    window.dispatchEvent(new Event(IN_TAB_EVENT));
  } catch {
    // Same swallow rationale as `reset()`. The in-memory state is
    // already updated; persistence is best-effort.
  }
}
