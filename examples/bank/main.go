package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/hydra-db/hydra/store"
)

const dataFile = "bank.log"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "open":
		if len(os.Args) < 3 {
			fmt.Println("Usage: bank open <account_id> [owner_name]")
			os.Exit(1)
		}
		accountID := os.Args[2]
		owner := accountID // Default owner to account ID
		if len(os.Args) >= 4 {
			owner = os.Args[3]
		}
		openAccount(accountID, owner)

	case "deposit":
		if len(os.Args) < 4 {
			fmt.Println("Usage: bank deposit <account_id> <amount> [note]")
			os.Exit(1)
		}
		accountID := os.Args[2]
		amount, err := strconv.ParseInt(os.Args[3], 10, 64)
		if err != nil {
			fmt.Printf("Invalid amount: %s\n", os.Args[3])
			os.Exit(1)
		}
		note := ""
		if len(os.Args) >= 5 {
			note = os.Args[4]
		}
		deposit(accountID, amount, note)

	case "withdraw":
		if len(os.Args) < 4 {
			fmt.Println("Usage: bank withdraw <account_id> <amount> [note]")
			os.Exit(1)
		}
		accountID := os.Args[2]
		amount, err := strconv.ParseInt(os.Args[3], 10, 64)
		if err != nil {
			fmt.Printf("Invalid amount: %s\n", os.Args[3])
			os.Exit(1)
		}
		note := ""
		if len(os.Args) >= 5 {
			note = os.Args[4]
		}
		withdraw(accountID, amount, note)

	case "balance":
		if len(os.Args) < 3 {
			fmt.Println("Usage: bank balance <account_id>")
			os.Exit(1)
		}
		accountID := os.Args[2]
		showBalance(accountID)

	case "history":
		if len(os.Args) < 3 {
			fmt.Println("Usage: bank history <account_id>")
			os.Exit(1)
		}
		accountID := os.Args[2]
		showHistory(accountID)

	case "all":
		showAllEvents()

	case "stats":
		showStats()

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Bank - Event Sourcing Demo")
	fmt.Println()
	fmt.Println("Usage: bank <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  open <account_id> [owner]     Open a new account")
	fmt.Println("  deposit <account_id> <amount> Deposit money")
	fmt.Println("  withdraw <account_id> <amount> Withdraw money")
	fmt.Println("  balance <account_id>          Show account balance (replays events)")
	fmt.Println("  history <account_id>          Show account event history")
	fmt.Println("  all                           Show all events in the log")
	fmt.Println("  stats                         Show log statistics")
}

func openAccount(accountID, owner string) {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Check if account already exists (stream has events)
	if s.StreamVersion(accountID) > 0 {
		fmt.Printf("Account %s already exists\n", accountID)
		os.Exit(1)
	}

	// Create and store the event
	event, err := NewAccountOpenedEvent(accountID, owner)
	if err != nil {
		fmt.Printf("Failed to create event: %v\n", err)
		os.Exit(1)
	}

	data, err := event.Serialize()
	if err != nil {
		fmt.Printf("Failed to serialize event: %v\n", err)
		os.Exit(1)
	}

	pos, version, err := s.Append(accountID, data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Account %s opened for %s (position=%d, version=%d)\n", accountID, owner, pos, version)
}

func deposit(accountID string, amount int64, note string) {
	if amount <= 0 {
		fmt.Println("Amount must be positive")
		os.Exit(1)
	}

	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Check if account exists
	account := replayAccount(s, accountID)
	if !account.Exists {
		fmt.Printf("Account %s does not exist. Open it first.\n", accountID)
		os.Exit(1)
	}

	// Create and store the event
	event, err := NewMoneyDepositedEvent(accountID, amount, note)
	if err != nil {
		fmt.Printf("Failed to create event: %v\n", err)
		os.Exit(1)
	}

	data, err := event.Serialize()
	if err != nil {
		fmt.Printf("Failed to serialize event: %v\n", err)
		os.Exit(1)
	}

	pos, version, err := s.Append(accountID, data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	newBalance := account.Balance + amount
	fmt.Printf("Deposited $%d to %s (balance=$%d, position=%d, version=%d)\n", amount, accountID, newBalance, pos, version)
}

func withdraw(accountID string, amount int64, note string) {
	if amount <= 0 {
		fmt.Println("Amount must be positive")
		os.Exit(1)
	}

	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Check if account exists and has sufficient balance
	account := replayAccount(s, accountID)
	if !account.Exists {
		fmt.Printf("Account %s does not exist. Open it first.\n", accountID)
		os.Exit(1)
	}
	if account.Balance < amount {
		fmt.Printf("Insufficient balance. Current balance: $%d, requested: $%d\n", account.Balance, amount)
		os.Exit(1)
	}

	// Create and store the event
	event, err := NewMoneyWithdrawnEvent(accountID, amount, note)
	if err != nil {
		fmt.Printf("Failed to create event: %v\n", err)
		os.Exit(1)
	}

	data, err := event.Serialize()
	if err != nil {
		fmt.Printf("Failed to serialize event: %v\n", err)
		os.Exit(1)
	}

	pos, version, err := s.Append(accountID, data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	newBalance := account.Balance - amount
	fmt.Printf("Withdrew $%d from %s (balance=$%d, position=%d, version=%d)\n", amount, accountID, newBalance, pos, version)
}

func showBalance(accountID string) {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	account := replayAccount(s, accountID)
	fmt.Println(account.String())
}

func showHistory(accountID string) {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Use ReadStream - efficient O(k) lookup!
	events, err := s.ReadStream(accountID)
	if err != nil {
		fmt.Printf("Failed to read stream: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Event history for account %s:\n", accountID)
	fmt.Println("---")

	for _, e := range events {
		event, err := DeserializeEvent(e.Data)
		if err != nil {
			continue
		}
		fmt.Printf("  v%d: %s\n", e.StreamVersion, event.String())
	}

	if len(events) == 0 {
		fmt.Println("  (no events found)")
	}
	fmt.Println("---")
	fmt.Printf("Total: %d events (current version: %d)\n", len(events), len(events))
}

func showAllEvents() {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	events, err := s.ReadAll()
	if err != nil {
		fmt.Printf("Failed to read all: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All events in the store:")
	fmt.Println("---")

	for i, e := range events {
		event, err := DeserializeEvent(e.Data)
		if err != nil {
			fmt.Printf("  #%d [pos=%d] <invalid>\n", i+1, e.GlobalPosition)
			continue
		}
		fmt.Printf("  #%d [pos=%d] [%s:v%d] %s\n", i+1, e.GlobalPosition, e.StreamID, e.StreamVersion, event.String())
	}

	if len(events) == 0 {
		fmt.Println("  (no events)")
	}
	fmt.Println("---")
	fmt.Printf("Total: %d events\n", len(events))
}

func showStats() {
	s, err := store.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	events, err := s.ReadAll()
	if err != nil {
		fmt.Printf("Failed to read all: %v\n", err)
		os.Exit(1)
	}

	// Count events by type and account
	accounts := make(map[string]int)
	eventTypes := make(map[EventType]int)

	for _, e := range events {
		accounts[e.StreamID]++
		event, err := DeserializeEvent(e.Data)
		if err != nil {
			continue
		}
		eventTypes[event.Type]++
	}

	fmt.Println("Store Statistics:")
	fmt.Println("---")
	fmt.Printf("Total events: %d\n", len(events))
	fmt.Println()
	fmt.Println("Events by type:")
	for t, count := range eventTypes {
		fmt.Printf("  %s: %d\n", t, count)
	}
	fmt.Println()
	fmt.Println("Events by account (stream):")
	for acc, count := range accounts {
		fmt.Printf("  %s: %d events (version %d)\n", acc, count, count)
	}
}

// replayAccount rebuilds account state by replaying stream events.
// NOW EFFICIENT: Uses ReadStream which is O(k) not O(n)!
func replayAccount(s *store.Store, accountID string) *Account {
	account := &Account{ID: accountID}

	events, err := s.ReadStream(accountID)
	if err != nil {
		return account
	}

	for _, e := range events {
		event, err := DeserializeEvent(e.Data)
		if err != nil {
			continue
		}
		account.Apply(event)
	}

	return account
}
