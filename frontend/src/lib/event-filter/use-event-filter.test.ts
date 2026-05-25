/**
 * Spec for `useEventFilter`. Pins the pivot toggle semantics that the
 * chip strip relies on:
 *
 *   1. Missing storage key → all families enabled (default).
 *   2. Toggle from "all" pivots to "that family only" (not subtracts).
 *   3. Toggle inside a subset adds/removes membership.
 *   4. Removing the last family snaps back to "all on".
 *   5. Adding the chip that completes the set snaps back to "all on".
 *   6. Persists across remounts.
 *   7. Malformed storage value → fallback to all-on (never crash).
 *
 * jsdom's `localStorage` is real, so we just clear it between specs.
 */
import { afterEach, describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useEventFilter } from "./use-event-filter";
import { FAMILIES } from "./families";

const STORAGE_KEY = "dfo:eventFilter";

afterEach(() => {
  window.localStorage.clear();
});

describe("useEventFilter", () => {
  it("defaults to every family enabled when the key is missing", () => {
    const { result } = renderHook(() => useEventFilter());
    expect(result.current.isAll).toBe(true);
    expect(result.current.active.size).toBe(FAMILIES.length);
    for (const f of FAMILIES) expect(result.current.active.has(f)).toBe(true);
  });

  it("toggles from 'all' pivot to 'that family only' (not subtract)", () => {
    const { result } = renderHook(() => useEventFilter());
    act(() => result.current.toggle("jobs"));
    expect(result.current.isAll).toBe(false);
    expect(result.current.active.size).toBe(1);
    expect(result.current.active.has("jobs")).toBe(true);
    expect(result.current.active.has("health")).toBe(false);
  });

  it("adds a second family inside a subset (multi-select)", () => {
    const { result } = renderHook(() => useEventFilter());
    act(() => result.current.toggle("jobs"));
    act(() => result.current.toggle("health"));
    expect(result.current.active.has("jobs")).toBe(true);
    expect(result.current.active.has("health")).toBe(true);
    expect(result.current.active.size).toBe(2);
  });

  it("snaps back to 'all on' when the last family is toggled off", () => {
    const { result } = renderHook(() => useEventFilter());
    act(() => result.current.toggle("jobs"));
    expect(result.current.active.size).toBe(1);
    act(() => result.current.toggle("jobs"));
    expect(result.current.isAll).toBe(true);
    expect(result.current.active.size).toBe(FAMILIES.length);
  });

  it("snaps to 'all on' when the toggle would complete the set", () => {
    const { result } = renderHook(() => useEventFilter());
    act(() => result.current.toggle("jobs"));
    for (const f of FAMILIES) {
      if (f === "jobs" || f === "health") continue;
      act(() => result.current.toggle(f));
    }
    expect(result.current.active.size).toBe(FAMILIES.length - 1);
    expect(result.current.active.has("health")).toBe(false);
    act(() => result.current.toggle("health"));
    expect(result.current.isAll).toBe(true);
  });

  it("persists toggles across remounts", () => {
    const first = renderHook(() => useEventFilter());
    act(() => first.result.current.toggle("jobs"));

    const second = renderHook(() => useEventFilter());
    expect(second.result.current.active.has("jobs")).toBe(true);
    expect(second.result.current.active.has("health")).toBe(false);
    expect(second.result.current.isAll).toBe(false);
  });

  it("falls back to all-on when the stored value is unrecognisable", () => {
    window.localStorage.setItem(STORAGE_KEY, "garbage,not-a-family,&&&");
    const { result } = renderHook(() => useEventFilter());
    expect(result.current.isAll).toBe(true);
    expect(result.current.active.size).toBe(FAMILIES.length);
  });

  it("preserves an explicit empty-string key as 'all hidden'", () => {
    window.localStorage.setItem(STORAGE_KEY, "");
    const { result } = renderHook(() => useEventFilter());
    expect(result.current.isAll).toBe(false);
    expect(result.current.active.size).toBe(0);
  });

  it("reset() clears the storage key and re-enables every family", () => {
    window.localStorage.setItem(STORAGE_KEY, "jobs");
    const { result } = renderHook(() => useEventFilter());
    expect(result.current.active.has("jobs")).toBe(true);
    expect(result.current.active.has("health")).toBe(false);

    act(() => result.current.reset());
    expect(result.current.isAll).toBe(true);
    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
  });
});
