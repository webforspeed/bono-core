package core

import "testing"

func TestConfigValidateSetsTurnDefaults(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if cfg.MaxChatTurns != 100 {
		t.Fatalf("MaxChatTurns = %d, want 100", cfg.MaxChatTurns)
	}
	if cfg.MaxPreTaskTurns != 100 {
		t.Fatalf("MaxPreTaskTurns = %d, want 100", cfg.MaxPreTaskTurns)
	}
	if cfg.MaxSubAgentTurns != 100 {
		t.Fatalf("MaxSubAgentTurns = %d, want 100", cfg.MaxSubAgentTurns)
	}
}

func TestConfigValidateDisableLimitsKeepsUnlimitedZeros(t *testing.T) {
	cfg := Config{
		DisableLimits:    true,
		MaxChatTurns:     0,
		MaxPreTaskTurns:  0,
		MaxSubAgentTurns: 0,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if cfg.MaxChatTurns != 0 {
		t.Fatalf("MaxChatTurns = %d, want 0", cfg.MaxChatTurns)
	}
	if cfg.MaxPreTaskTurns != 0 {
		t.Fatalf("MaxPreTaskTurns = %d, want 0", cfg.MaxPreTaskTurns)
	}
	if cfg.MaxSubAgentTurns != 0 {
		t.Fatalf("MaxSubAgentTurns = %d, want 0", cfg.MaxSubAgentTurns)
	}
}
