/**
 * Single import surface for the live polling layer.
 *
 *   import { useLiveState, useLiveRunState } from "@/lib/live";
 *
 * Components don't need anything else from this module; the reducers
 * + helpers are wired internally by the hooks via `applyEvent`.
 */
export { useLiveState } from "./use-live-state";
export { useLiveRunState } from "./use-live-run-state";
