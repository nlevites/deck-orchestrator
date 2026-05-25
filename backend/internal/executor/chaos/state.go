// Package chaos is the executor's runtime failure-injection surface.
// One State per process, shared by worker, outbox, egress transport, and ingress handler.
// All flags guarded by sync.RWMutex; lock held only for field reads/writes, never across blocking work.
package chaos

import "sync"

// State holds in-process chaos flags. hangChange closes on mutations that may
// release a worker parked in hang, then is replaced so subsequent hangs re-arm.
// Callers must re-fetch HangChangedCh() after each wake — never cache across releases.
type State struct {
	mu             sync.RWMutex
	hang           bool
	hangAfterStep  int
	crashAfterStep int
	silent         bool
	dropEvents     bool
	pauseEgress    bool
	pauseIngress   bool
	hangChange     chan struct{}
}

// InitialState seeds or patches State. Pointer fields distinguish JSON absent
// from present-and-zero: nil = leave as-is, non-nil = set to pointed value.
type InitialState struct {
	Hang           *bool `json:"hang,omitempty"`
	HangAfterStep  *int  `json:"hang_after_step,omitempty"`
	CrashAfterStep *int  `json:"crash_after_step,omitempty"`
	Silent         *bool `json:"silent,omitempty"`
	DropEvents     *bool `json:"drop_events,omitempty"`
	PauseEgress    *bool `json:"pause_egress,omitempty"`
	PauseIngress   *bool `json:"pause_ingress,omitempty"`
}

// Snapshot is a complete view (plain values, not patch pointers).
type Snapshot struct {
	Hang           bool `json:"hang"`
	HangAfterStep  int  `json:"hang_after_step"`
	CrashAfterStep int  `json:"crash_after_step"`
	Silent         bool `json:"silent"`
	DropEvents     bool `json:"drop_events"`
	PauseEgress    bool `json:"pause_egress"`
	PauseIngress   bool `json:"pause_ingress"`
}

func New(init InitialState) *State {
	s := &State{hangChange: make(chan struct{})}
	s.Apply(init)
	return s
}

// Apply merges patch; nil fields untouched. Patches touching Hang release
// waiters (Hang=true is harmless wakeup; Hang=false is the meaningful release).
func (s *State) Apply(patch InitialState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if patch.Hang != nil {
		s.hang = *patch.Hang
	}
	if patch.HangAfterStep != nil {
		s.hangAfterStep = *patch.HangAfterStep
	}
	if patch.CrashAfterStep != nil {
		s.crashAfterStep = *patch.CrashAfterStep
	}
	if patch.Silent != nil {
		s.silent = *patch.Silent
	}
	if patch.DropEvents != nil {
		s.dropEvents = *patch.DropEvents
	}
	if patch.PauseEgress != nil {
		s.pauseEgress = *patch.PauseEgress
	}
	if patch.PauseIngress != nil {
		s.pauseIngress = *patch.PauseIngress
	}
	if patch.Hang != nil {
		s.notifyHangChangeLocked()
	}
}

// Reset zeroes all flags and releases hang waiters.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hang = false
	s.hangAfterStep = 0
	s.crashAfterStep = 0
	s.silent = false
	s.dropEvents = false
	s.pauseEgress = false
	s.pauseIngress = false
	s.notifyHangChangeLocked()
}

func (s *State) SnapshotState() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Snapshot{
		Hang:           s.hang,
		HangAfterStep:  s.hangAfterStep,
		CrashAfterStep: s.crashAfterStep,
		Silent:         s.silent,
		DropEvents:     s.dropEvents,
		PauseEgress:    s.pauseEgress,
		PauseIngress:   s.pauseIngress,
	}
}

func (s *State) Hang() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hang
}

// SetHang updates immediate hang; worker calls this when hangAfterStep fires.
func (s *State) SetHang(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hang = v
	s.notifyHangChangeLocked()
}

// HangChangedCh is single-use: closed on Reset, SetHang, or Apply(Hang), then replaced.
func (s *State) HangChangedCh() <-chan struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hangChange
}

// notifyHangChangeLocked requires s.mu held for write.
func (s *State) notifyHangChangeLocked() {
	close(s.hangChange)
	s.hangChange = make(chan struct{})
}

func (s *State) HangAfterStep() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hangAfterStep
}

func (s *State) CrashAfterStep() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.crashAfterStep
}

func (s *State) Silent() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.silent
}

func (s *State) DropEvents() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dropEvents
}

func (s *State) PauseEgress() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pauseEgress
}

func (s *State) PauseIngress() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pauseIngress
}
