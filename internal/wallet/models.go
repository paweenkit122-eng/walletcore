// Package wallet holds the ledger domain: accounts, transactions, entries,
// and the operations that move money between them (top-up, transfer).
//
// All money amounts are integers in the smallest currency unit (satang for
// THB) — never floats — so arithmetic is exact.
package wallet

import "time"

// Account is a wallet. It can belong to a user ("user") or be an internal
// system account ("system", e.g. the source external top-ups flow from).
type Account struct {
	ID        int64
	OwnerType string // "user" | "system"
	OwnerID   *int64 // nil for system accounts
	Name      string // e.g. "wallet", "topup_source"
	Currency  string // e.g. "THB"
	Balance   int64  // satang
	CreatedAt time.Time
}

// Transaction is one business event (a top-up or a transfer) that groups two
// or more ledger entries together. IdempotencyKey is UNIQUE in the DB, which
// is how retries are made safe.
type Transaction struct {
	ID             int64
	Kind           string // "topup" | "transfer"
	IdempotencyKey string
	Reference      string
	CreatedAt      time.Time
}

// Entry is a single double-entry ledger line.
// Convention: Amount > 0 is a CREDIT (money IN), Amount < 0 is a DEBIT (money OUT).
// INVARIANT: for every transaction, the sum of its entries' Amount is exactly 0.
type Entry struct {
	ID            int64
	TransactionID int64
	AccountID     int64
	Amount        int64 // satang; +credit / -debit
	CreatedAt     time.Time
}
