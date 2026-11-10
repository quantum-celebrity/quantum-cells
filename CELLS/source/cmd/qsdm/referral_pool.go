package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	CELLSReferralRewardPoolSeedEnv           = "CELLS_REFERRAL_REWARD_POOL_SEED_CELL"
	CELLSReferralRewardPoolAllowLocalSeedEnv = "CELLS_REFERRAL_REWARD_POOL_ALLOW_LOCAL_SEED"
)

// rejectLegacyReferralRewardPoolSeed prevents old launchers from silently
// manufacturing referral balances. Production pools must be funded through a
// normal signed transfer to the configured treasury signer wallet.
func rejectLegacyReferralRewardPoolSeed() error {
	if strings.TrimSpace(os.Getenv(CELLSReferralRewardPoolSeedEnv)) == "" &&
		strings.TrimSpace(os.Getenv(CELLSReferralRewardPoolAllowLocalSeedEnv)) == "" {
		return nil
	}
	return fmt.Errorf("%s and %s are retired; fund the referral treasury with a signed wallet transfer",
		CELLSReferralRewardPoolSeedEnv, CELLSReferralRewardPoolAllowLocalSeedEnv)
}
