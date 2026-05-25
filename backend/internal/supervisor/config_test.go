package supervisor

import (
	"strings"
	"testing"
)

func baseConfig() Config {
	return Config{
		StateDir: "/tmp/sup-test",
		Orchestrator: OrchestratorSpec{
			BinaryPath: "/bin/orchestrator",
		},
		Executor: ExecutorSpec{
			BinaryPath: "/bin/executor",
			PortBase:   9000,
		},
	}
}

func TestValidate_ExecutorCountExpands(t *testing.T) {
	cfg := baseConfig()
	cfg.ExecutorCount = 3
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"deck-1", "deck-2", "deck-3"}
	if len(cfg.PrelaunchExecutors) != len(want) {
		t.Fatalf("want %d prelaunch entries, got %d (%v)", len(want), len(cfg.PrelaunchExecutors), cfg.PrelaunchExecutors)
	}
	for i, w := range want {
		if cfg.PrelaunchExecutors[i] != w {
			t.Errorf("PrelaunchExecutors[%d] = %q, want %q", i, cfg.PrelaunchExecutors[i], w)
		}
	}
}

func TestValidate_ExecutorCountZeroIsNoOp(t *testing.T) {
	cfg := baseConfig()
	cfg.PrelaunchExecutors = []string{"deck-7"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.PrelaunchExecutors) != 1 || cfg.PrelaunchExecutors[0] != "deck-7" {
		t.Errorf("PrelaunchExecutors mutated: %v", cfg.PrelaunchExecutors)
	}
}

func TestValidate_ExecutorCountAndPrelaunchMutuallyExclusive(t *testing.T) {
	cfg := baseConfig()
	cfg.ExecutorCount = 3
	cfg.PrelaunchExecutors = []string{"deck-99"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error did not mention mutual exclusion: %v", err)
	}
}

func TestValidate_ExecutorCountNegativeRejected(t *testing.T) {
	cfg := baseConfig()
	cfg.ExecutorCount = -1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "executor_count") {
		t.Errorf("error did not mention executor_count: %v", err)
	}
}
