/**
 * Vitest global setup. Registers `@testing-library/jest-dom` matchers so
 * `expect(node).toBeInTheDocument()` etc. works in component tests, and
 * resets the DOM between specs to keep tests isolated.
 *
 * Wired in via vite.config.ts `test.setupFiles`.
 */
import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

afterEach(() => {
  cleanup();
});
