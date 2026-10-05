package config

import (
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/auth"
)

// ChatGPTAccountsConfig is global-only operator policy. Aliases are discovered
// from the credential store, not registered in configuration. Project files
// cannot select an account or enable automatic failover.
type ChatGPTAccountsConfig struct {
	Preferred string   `json:"preferred,omitempty"`
	Fallback  []string `json:"fallback,omitempty"`
}

func (c ChatGPTAccountsConfig) Validate() error {
	if c.Preferred != "" {
		if err := auth.ValidateAccountName(c.Preferred); err != nil {
			return fmt.Errorf("config: chatgpt_accounts preferred: %w", err)
		}
	}
	if len(c.Fallback) != 0 {
		return errors.New("config: chatgpt_accounts fallback: automatic account failover is not supported until its contract is verified")
	}
	return nil
}
