package chaos_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/executor/chaos"
)

func b(v bool) *bool { return &v }
func i(v int) *int   { return &v }

func TestState_ZeroValueIsNoChaos(t *testing.T) {
	s := chaos.New(chaos.InitialState{})
	snap := s.SnapshotState()
	require.False(t, snap.Hang)
	require.False(t, snap.Silent)
	require.False(t, snap.DropEvents)
	require.False(t, snap.PauseEgress)
	require.False(t, snap.PauseIngress)
	require.Zero(t, snap.HangAfterStep)
	require.Zero(t, snap.CrashAfterStep)
}

func TestState_ApplyMergesPatch(t *testing.T) {
	s := chaos.New(chaos.InitialState{Hang: b(true), HangAfterStep: i(3)})
	require.True(t, s.Hang())
	require.Equal(t, 3, s.HangAfterStep())

	s.Apply(chaos.InitialState{Silent: b(true)})
	require.True(t, s.Hang(), "Apply must preserve unset fields")
	require.True(t, s.Silent())

	s.Apply(chaos.InitialState{Hang: b(false)})
	require.False(t, s.Hang())
	require.True(t, s.Silent())
}

func TestState_Reset(t *testing.T) {
	s := chaos.New(chaos.InitialState{
		Hang:           b(true),
		HangAfterStep:  i(2),
		CrashAfterStep: i(5),
		Silent:         b(true),
		DropEvents:     b(true),
		PauseEgress:    b(true),
		PauseIngress:   b(true),
	})
	s.Reset()
	snap := s.SnapshotState()
	require.False(t, snap.Hang)
	require.False(t, snap.Silent)
	require.Zero(t, snap.HangAfterStep)
	require.Zero(t, snap.CrashAfterStep)
}

// assertClosed fails if ch is not already closed (avoids hanging the suite).
func assertClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-ch:
		require.False(t, ok, "channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected hangChange channel to be closed, but recv timed out")
	}
}

// assertOpen fails if ch is not blocking (short timeout keeps tests snappy).
func assertOpen(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("expected hangChange channel to be open, but recv returned")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestHangChangedCh_closesOnReset(t *testing.T) {
	s := chaos.New(chaos.InitialState{Hang: b(true)})
	ch := s.HangChangedCh()
	assertOpen(t, ch)

	s.Reset()
	assertClosed(t, ch)

	next := s.HangChangedCh()
	require.NotNil(t, next)
	assertOpen(t, next)
}

func TestHangChangedCh_closesOnSetHang(t *testing.T) {
	s := chaos.New(chaos.InitialState{})

	ch1 := s.HangChangedCh()
	s.SetHang(true)
	assertClosed(t, ch1)

	ch2 := s.HangChangedCh()
	assertOpen(t, ch2)
	s.SetHang(false)
	assertClosed(t, ch2)

	ch3 := s.HangChangedCh()
	assertOpen(t, ch3)
}

func TestHangChangedCh_closesOnApplyHang(t *testing.T) {
	s := chaos.New(chaos.InitialState{Hang: b(true)})

	ch := s.HangChangedCh()
	assertOpen(t, ch)

	s.Apply(chaos.InitialState{Silent: b(true)})
	assertOpen(t, ch)

	s.Apply(chaos.InitialState{Hang: b(false)})
	assertClosed(t, ch)
}

func TestState_ConcurrentAccess(t *testing.T) {
	s := chaos.New(chaos.InitialState{})
	var wg sync.WaitGroup

	const n = 8
	wg.Add(n * 2)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				s.Apply(chaos.InitialState{Hang: b(k%2 == 0)})
			}
		}()
	}
	for r := 0; r < n; r++ {
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				_ = s.Hang()
				_ = s.HangAfterStep()
				_ = s.SnapshotState()
			}
		}()
	}
	wg.Wait()
}

func TestEgressTransport_PausesAndResumes(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	state := chaos.New(chaos.InitialState{})
	tr := chaos.NewEgressTransport(http.DefaultTransport, state)
	c := &http.Client{Transport: tr}

	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 1, hits, "request should land through unwrapped transport")

	state.Apply(chaos.InitialState{PauseEgress: b(true)})
	_, err = c.Get(srv.URL) //nolint:bodyclose // expected error path returns nil resp
	require.ErrorIs(t, err, chaos.ErrEgressPaused)
	require.Equal(t, 1, hits, "paused request must not reach the server")

	state.Apply(chaos.InitialState{PauseEgress: b(false)})
	resp, err = c.Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 2, hits, "resumed request lands again")
}

func TestEgressTransport_NilStateIsAlwaysOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tr := chaos.NewEgressTransport(http.DefaultTransport, nil)
	c := &http.Client{Transport: tr}
	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
}

func TestIngressHandler_PausesNonChaosRoutes(t *testing.T) {
	state := chaos.New(chaos.InitialState{})
	inner := http.NewServeMux()
	inner.HandleFunc("/executor/state", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	inner.HandleFunc("/executor/chaos", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	inner.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(chaos.NewIngressHandler(inner, state))
	t.Cleanup(srv.Close)

	c := &http.Client{}

	resp, err := c.Get(srv.URL + "/executor/state")
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	state.Apply(chaos.InitialState{PauseIngress: b(true)})
	//nolint:bodyclose // err path means no body
	_, err = c.Get(srv.URL + "/executor/state")
	require.Error(t, err, "paused server must drop the connection")

	resp, err = c.Get(srv.URL + "/executor/chaos")
	require.NoError(t, err, "chaos route must bypass pause")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = c.Get(srv.URL + "/health")
	require.NoError(t, err, "health route must bypass pause")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
