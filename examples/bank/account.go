package main

import (
	"encoding/json"
	"fmt"
)

// Account represents the current state of a bank account
// This state is rebuilt by replaying events
type Account struct {
	ID      string
	Owner   string
	Balance int64
	Exists  bool
}

// Apply applies an event to update the account state
func (a *Account) Apply(event *Event) error {
	// Only apply events for this account
	if event.AccountID != a.ID {
		return nil
	}

	switch event.Type {
	case EventAccountOpened:
		var data AccountOpenedData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return fmt.Errorf("failed to unmarshal AccountOpened: %w", err)
		}
		a.Owner = data.Owner
		a.Exists = true

	case EventMoneyDeposited:
		var data MoneyDepositedData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return fmt.Errorf("failed to unmarshal MoneyDeposited: %w", err)
		}
		a.Balance += data.Amount

	case EventMoneyWithdrawn:
		var data MoneyWithdrawnData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return fmt.Errorf("failed to unmarshal MoneyWithdrawn: %w", err)
		}
		a.Balance -= data.Amount
	}

	return nil
}

// String returns a human-readable representation of the account
func (a *Account) String() string {
	if !a.Exists {
		return fmt.Sprintf("Account %s: does not exist", a.ID)
	}
	return fmt.Sprintf("Account %s (owner: %s): Balance = $%d", a.ID, a.Owner, a.Balance)
}
