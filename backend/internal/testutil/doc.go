// Package testutil is shared test fixtures for deck-fleet backend tests.
//
// Conventions:
//   - Helpers take *testing.T first and t.Fatal on setup error.
//   - Epoch is the default timestamp; FakeClock advances time without sleeping.
//   - Functional options keep call sites focused.
package testutil

import "time"

// Epoch is the default timestamp for seeded DB rows.
var Epoch = time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
