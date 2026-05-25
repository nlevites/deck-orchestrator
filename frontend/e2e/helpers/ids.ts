import type { TestInfo } from "@playwright/test";

/**
 * Unique run ID per test — title slug + random suffix for --repeat-each safety.
 */
export function runIdFor(testInfo: TestInfo, kind: string): string {
  const slug = testInfo.title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 32);
  const rnd = Math.random().toString(36).slice(2, 6);
  return `${kind}-${slug}-${rnd}`;
}
