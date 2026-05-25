package dispatch

import "sync"

// notify wakes long-poll handlers when a deck has new dispatched work.
// Package-level singleton — there is exactly one dispatcher per
// orchestrator process and the long-poll handler doesn't need a
// dependency-injected handle. Subscribers are short-lived per-request
// goroutines.
//
// Each Subscribe gets a 1-buffered channel. Notify drops sends that
// would block (the subscriber already has a wake pending). Loss of a
// notification is bounded: the long-poll handler always re-queries
// the DB after wake AND wakes itself periodically as a safety net
// (see handlers/executor_poll.go).

type deckListeners struct {
	mu  sync.Mutex
	chs []chan struct{}
}

var brokerMap sync.Map // map[string]*deckListeners (deckID -> listeners)

// Subscribe registers a wake channel for deckID. The returned cancel
// function MUST be called when the subscriber exits (defer is fine).
func Subscribe(deckID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	v, _ := brokerMap.LoadOrStore(deckID, &deckListeners{})
	dl := v.(*deckListeners)
	dl.mu.Lock()
	dl.chs = append(dl.chs, ch)
	dl.mu.Unlock()
	return ch, func() {
		dl.mu.Lock()
		for i, c := range dl.chs {
			if c == ch {
				dl.chs = append(dl.chs[:i], dl.chs[i+1:]...)
				break
			}
		}
		dl.mu.Unlock()
	}
}

// NotifyDeck wakes every subscriber of deckID. Callers fire this AFTER
// the enclosing tx commits — otherwise the woken /executor/poll handler
// can re-query before the dispatch is visible and burn its safety-net
// tick before the row actually exists. Non-blocking; safe to call in a
// tight post-commit loop.
func NotifyDeck(deckID string) {
	v, ok := brokerMap.Load(deckID)
	if !ok {
		return
	}
	dl := v.(*deckListeners)
	dl.mu.Lock()
	for _, ch := range dl.chs {
		select {
		case ch <- struct{}{}:
		default: // already has a pending wake; drop
		}
	}
	dl.mu.Unlock()
}

// NotifyDecks is a convenience for post-commit wake of a batch of deck
// ids. Duplicates are harmless (notify is idempotent).
func NotifyDecks(deckIDs []string) {
	for _, id := range deckIDs {
		NotifyDeck(id)
	}
}
