package main

import (
	"testing"
)

func TestRejectLegacyReferralRewardPoolSeed(t *testing.T) {
	t.Setenv(CELLSReferralRewardPoolSeedEnv, "500")
	if err := rejectLegacyReferralRewardPoolSeed(); err == nil {
		t.Fatal("expected legacy seed configuration to be rejected")
	}
}

func TestRejectLegacyReferralRewardPoolLocalAllowFlag(t *testing.T) {
	t.Setenv(CELLSReferralRewardPoolAllowLocalSeedEnv, "1")
	if err := rejectLegacyReferralRewardPoolSeed(); err == nil {
		t.Fatal("expected retired local-seed flag to be rejected")
	}
}

func TestRejectLegacyReferralRewardPoolSeedAllowsCleanProductionConfig(t *testing.T) {
	if err := rejectLegacyReferralRewardPoolSeed(); err != nil {
		t.Fatalf("clean production configuration rejected: %v", err)
	}
}
