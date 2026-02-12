package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/hydra-db/hydra/log"
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
	l, err := log.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open log: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	// Check if account already exists
	account := replayAccount(l, accountID)
	if account.Exists {
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

	pos, err := l.Append(data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Account %s opened for %s (stored at position %d)\n", accountID, owner, pos)
}

func deposit(accountID string, amount int64, note string) {
	if amount <= 0 {
		fmt.Println("Amount must be positive")
		os.Exit(1)
	}

	l, err := log.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open log: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	// Check if account exists
	account := replayAccount(l, accountID)
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

	pos, err := l.Append(data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	newBalance := account.Balance + amount
	fmt.Printf("Deposited $%d to %s (new balance: $%d, stored at position %d)\n", amount, accountID, newBalance, pos)
}

func withdraw(accountID string, amount int64, note string) {
	if amount <= 0 {
		fmt.Println("Amount must be positive")
		os.Exit(1)
	}

	l, err := log.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open log: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	// Check if account exists and has sufficient balance
	account := replayAccount(l, accountID)
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

	pos, err := l.Append(data)
	if err != nil {
		fmt.Printf("Failed to append event: %v\n", err)
		os.Exit(1)
	}

	newBalance := account.Balance - amount
	fmt.Printf("Withdrew $%d from %s (new balance: $%d, stored at position %d)\n", amount, accountID, newBalance, pos)
}

func showBalance(accountID string) {
	l, err := log.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open log: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	account := replayAccount(l, accountID)
	fmt.Println(account.String())
}

func showHistory(accountID string) {
	l, err := log.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open log: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	records, err := l.ReadAll()
	if err != nil {
		fmt.Printf("Failed to read log: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Event history for account %s:\n", accountID)
	fmt.Println("---")

	count := 0
	for _, record := range records {
		event, err := DeserializeEvent(record.Data)
		if err != nil {
			continue
		}
		if event.AccountID == accountID {
			fmt.Printf("  %s\n", event.String())
			count++
		}
	}

	if count == 0 {
		fmt.Println("  (no events found)")
	}
	fmt.Println("---")
	fmt.Printf("Total: %d events\n", count)
}

func showAllEvents() {
	l, err := log.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open log: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	records, err := l.ReadAll()
	if err != nil {
		fmt.Printf("Failed to read log: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All events in the log:")
	fmt.Println("---")

	for i, record := range records {
		event, err := DeserializeEvent(record.Data)
		if err != nil {
			fmt.Printf("  #%d [pos=%d] <corrupt or invalid>\n", i+1, record.Position)
			continue
		}
		fmt.Printf("  #%d [pos=%d] [%s] %s\n", i+1, record.Position, event.AccountID, event.String())
	}

	if len(records) == 0 {
		fmt.Println("  (no events)")
	}
	fmt.Println("---")
	fmt.Printf("Total: %d events\n", len(records))
}

func showStats() {
	l, err := log.Open(dataFile)
	if err != nil {
		fmt.Printf("Failed to open log: %v\n", err)
		os.Exit(1)
	}
	defer l.Close()

	records, err := l.ReadAll()
	if err != nil {
		fmt.Printf("Failed to read log: %v\n", err)
		os.Exit(1)
	}

	// Count events by type and account
	accounts := make(map[string]int)
	eventTypes := make(map[EventType]int)

	var totalBytes int64 = 0
	for _, record := range records {
		totalBytes += int64(log.HeaderSize + len(record.Data))
		event, err := DeserializeEvent(record.Data)
		if err != nil {
			continue
		}
		accounts[event.AccountID]++
		eventTypes[event.Type]++
	}

	fmt.Println("Log Statistics:")
	fmt.Println("---")
	fmt.Printf("Total events: %d\n", len(records))
	fmt.Printf("Total size: %d bytes\n", totalBytes)
	fmt.Println()
	fmt.Println("Events by type:")
	for t, count := range eventTypes {
		fmt.Printf("  %s: %d\n", t, count)
	}
	fmt.Println()
	fmt.Println("Events by account:")
	for acc, count := range accounts {
		fmt.Printf("  %s: %d\n", acc, count)
	}
}

// replayAccount rebuilds account state by replaying all events
// NOTE: In Phase 1, we scan ALL events. Phase 2 will add indexing for efficiency.
func replayAccount(l *log.Log, accountID string) *Account {
	account := &Account{ID: accountID}

	records, err := l.ReadAll()
	if err != nil {
		return account
	}

	for _, record := range records {
		event, err := DeserializeEvent(record.Data)
		if err != nil {
			continue
		}
		account.Apply(event)
	}

	return account
}
