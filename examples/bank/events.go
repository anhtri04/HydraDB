package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// EventType identifies the type of event
type EventType string

const (
	EventAccountOpened   EventType = "AccountOpened"
	EventMoneyDeposited  EventType = "MoneyDeposited"
	EventMoneyWithdrawn  EventType = "MoneyWithdrawn"
)

// Event is the envelope that wraps all events
type Event struct {
	Type      EventType       `json:"type"`
	AccountID string          `json:"account_id"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// AccountOpenedData is the payload for AccountOpened events
type AccountOpenedData struct {
	Owner string `json:"owner"`
}

// MoneyDepositedData is the payload for MoneyDeposited events
type MoneyDepositedData struct {
	Amount int64  `json:"amount"`
	Note   string `json:"note,omitempty"`
}

// MoneyWithdrawnData is the payload for MoneyWithdrawn events
type MoneyWithdrawnData struct {
	Amount int64  `json:"amount"`
	Note   string `json:"note,omitempty"`
}

// NewAccountOpenedEvent creates an AccountOpened event
func NewAccountOpenedEvent(accountID, owner string) (*Event, error) {
	data, err := json.Marshal(AccountOpenedData{Owner: owner})
	if err != nil {
		return nil, err
	}
	return &Event{
		Type:      EventAccountOpened,
		AccountID: accountID,
		Timestamp: time.Now(),
		Data:      data,
	}, nil
}

// NewMoneyDepositedEvent creates a MoneyDeposited event
func NewMoneyDepositedEvent(accountID string, amount int64, note string) (*Event, error) {
	data, err := json.Marshal(MoneyDepositedData{Amount: amount, Note: note})
	if err != nil {
		return nil, err
	}
	return &Event{
		Type:      EventMoneyDeposited,
		AccountID: accountID,
		Timestamp: time.Now(),
		Data:      data,
	}, nil
}

// NewMoneyWithdrawnEvent creates a MoneyWithdrawn event
func NewMoneyWithdrawnEvent(accountID string, amount int64, note string) (*Event, error) {
	data, err := json.Marshal(MoneyWithdrawnData{Amount: amount, Note: note})
	if err != nil {
		return nil, err
	}
	return &Event{
		Type:      EventMoneyWithdrawn,
		AccountID: accountID,
		Timestamp: time.Now(),
		Data:      data,
	}, nil
}

// Serialize converts an event to bytes for storage
func (e *Event) Serialize() ([]byte, error) {
	return json.Marshal(e)
}

// DeserializeEvent converts bytes back to an Event
func DeserializeEvent(data []byte) (*Event, error) {
	var e Event
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// String returns a human-readable representation of the event
func (e *Event) String() string {
	switch e.Type {
	case EventAccountOpened:
		var d AccountOpenedData
		json.Unmarshal(e.Data, &d)
		return fmt.Sprintf("[%s] Account opened for %s", e.Timestamp.Format("15:04:05"), d.Owner)
	case EventMoneyDeposited:
		var d MoneyDepositedData
		json.Unmarshal(e.Data, &d)
		if d.Note != "" {
			return fmt.Sprintf("[%s] Deposited $%d (%s)", e.Timestamp.Format("15:04:05"), d.Amount, d.Note)
		}
		return fmt.Sprintf("[%s] Deposited $%d", e.Timestamp.Format("15:04:05"), d.Amount)
	case EventMoneyWithdrawn:
		var d MoneyWithdrawnData
		json.Unmarshal(e.Data, &d)
		if d.Note != "" {
			return fmt.Sprintf("[%s] Withdrew $%d (%s)", e.Timestamp.Format("15:04:05"), d.Amount, d.Note)
		}
		return fmt.Sprintf("[%s] Withdrew $%d", e.Timestamp.Format("15:04:05"), d.Amount)
	default:
		return fmt.Sprintf("[%s] Unknown event: %s", e.Timestamp.Format("15:04:05"), e.Type)
	}
}
