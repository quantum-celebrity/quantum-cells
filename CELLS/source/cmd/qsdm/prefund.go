package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/blackbeardONE/CELLS/pkg/chain"
	"github.com/blackbeardONE/CELLS/pkg/envcompat"
)

const CELLSPrefundAccountsEnv = "CELLS_PREFUND_ACCOUNTS"
const CELLSAllowDevelopmentPrefundEnv = "CELLS_ALLOW_DEVELOPMENT_PREFUND"
const CELLSProductionModeEnv = "CELLS_PRODUCTION_MODE"

var developmentFundingEnvKeys = []string{
	CELLSPrefundAccountsEnv,
	"CELLS_GENESIS_PREFUND_ADDR",
	"CELLS_GENESIS_PREFUND_AMOUNT_CELL",
}

type prefundAccount struct {
	Address string
	Amount  float64
}

func rejectDevelopmentFundingInProduction() error {
	if !envcompat.Truthy(CELLSProductionModeEnv, CELLSProductionModeEnv) {
		return nil
	}
	for _, key := range developmentFundingEnvKeys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return fmt.Errorf("%s is forbidden when %s=true", key, CELLSProductionModeEnv)
		}
	}
	return nil
}

func parsePrefundAccounts(raw string) ([]prefundAccount, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	entries := make([]prefundAccount, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		sep := strings.IndexAny(part, ":=")
		if sep <= 0 || sep == len(part)-1 {
			return nil, fmt.Errorf("invalid prefund entry %q, want address:amount", part)
		}
		address := strings.TrimSpace(part[:sep])
		amountRaw := strings.TrimSpace(part[sep+1:])
		if address == "" {
			return nil, fmt.Errorf("invalid prefund entry %q: address is required", part)
		}
		amount, err := strconv.ParseFloat(amountRaw, 64)
		if err != nil || amount <= 0 {
			return nil, fmt.Errorf("invalid prefund amount %q for %s", amountRaw, address)
		}
		entries = append(entries, prefundAccount{
			Address: address,
			Amount:  amount,
		})
	}

	return entries, nil
}

func applyPrefundAccounts(accounts *chain.AccountStore, raw string) ([]prefundAccount, error) {
	if accounts == nil {
		return nil, fmt.Errorf("account store is nil")
	}
	entries, err := parsePrefundAccounts(raw)
	if err != nil {
		return nil, err
	}
	if len(entries) > 0 && !envcompat.Truthy(CELLSAllowDevelopmentPrefundEnv, CELLSAllowDevelopmentPrefundEnv) {
		return nil, fmt.Errorf("%s is development-only and requires %s=1", CELLSPrefundAccountsEnv, CELLSAllowDevelopmentPrefundEnv)
	}
	if len(entries) > 0 && envcompat.Truthy(CELLSProductionModeEnv, CELLSProductionModeEnv) {
		return nil, fmt.Errorf("%s is forbidden when %s=true", CELLSPrefundAccountsEnv, CELLSProductionModeEnv)
	}
	for _, entry := range entries {
		accounts.Credit(entry.Address, entry.Amount)
	}
	return entries, nil
}
