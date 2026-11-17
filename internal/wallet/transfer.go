package wallet

import (
	"context"
	"errors"
	"fmt"
)

// Errors returned by Transfer.
var (
	ErrSameAccount       = errors.New("source and destination accounts must differ")
	ErrAccountNotFound   = errors.New("account not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// TransferResult is what a successful (or replayed) transfer returns.
type TransferResult struct {
	TransactionID int64
	FromAccountID int64
	ToAccountID   int64
	FromBalance   int64 // sender balance after the transfer, in satang
	ToBalance     int64 // receiver balance after the transfer, in satang
}

// Transfer moves `amount` satang from one account to another as ONE double-entry
// transaction. It is:
//   - idempotent      — replaying idempotencyKey returns the original result,
//     money is never moved twice;
//   - concurrency-safe — both account rows are locked with SELECT … FOR UPDATE
//     in a fixed order (by id) so concurrent transfers can't lose an update or
//     deadlock against each other;
//   - overdraft-safe   — a transfer is rejected (ErrInsufficientFunds) if the
//     sender doesn't have enough, checked while holding the lock.
func (s *Store) Transfer(ctx context.Context, fromAccountID, toAccountID, amount int64, idempotencyKey string) (TransferResult, error) {
	if amount <= 0 {
		return TransferResult{}, ErrInvalidAmount
	}
	if fromAccountID == toAccountID {
		return TransferResult{}, ErrSameAccount
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TransferResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Record the business event; the UNIQUE idempotency_key guards retries.
	var txID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (kind, idempotency_key) VALUES ('transfer', $1) RETURNING id`,
		idempotencyKey,
	).Scan(&txID)
	if isUniqueViolation(err) {
		_ = tx.Rollback(ctx)
		return s.replayTransfer(ctx, idempotencyKey)
	}
	if err != nil {
		return TransferResult{}, fmt.Errorf("insert transaction: %w", err)
	}

	// Lock BOTH account rows before reading balances. ORDER BY id gives every
	// transfer the same lock order, so two transfers between the same pair (even
	// in opposite directions) line up instead of deadlocking.
	rows, err := tx.Query(ctx,
		`SELECT id, balance FROM accounts WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`,
		fromAccountID, toAccountID,
	)
	if err != nil {
		return TransferResult{}, fmt.Errorf("lock accounts: %w", err)
	}
	balances := make(map[int64]int64, 2)
	for rows.Next() {
		var id, bal int64
		if err := rows.Scan(&id, &bal); err != nil {
			rows.Close()
			return TransferResult{}, fmt.Errorf("scan balance: %w", err)
		}
		balances[id] = bal
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return TransferResult{}, fmt.Errorf("read balances: %w", err)
	}

	// Both accounts must exist…
	if _, ok := balances[fromAccountID]; !ok {
		return TransferResult{}, ErrAccountNotFound
	}
	if _, ok := balances[toAccountID]; !ok {
		return TransferResult{}, ErrAccountNotFound
	}
	// …and the sender must have enough money.
	if balances[fromAccountID] < amount {
		return TransferResult{}, ErrInsufficientFunds
	}

	// Two ledger lines, summing to zero: money leaves the sender, arrives at the receiver.
	if _, err = tx.Exec(ctx,
		`INSERT INTO entries (transaction_id, account_id, amount)
		 VALUES ($1, $2, $3), ($1, $4, $5)`,
		txID, fromAccountID, -amount, toAccountID, amount,
	); err != nil {
		return TransferResult{}, fmt.Errorf("insert entries: %w", err)
	}

	// Move the materialized balances (safe: we hold both row locks).
	var fromBalance, toBalance int64
	if err = tx.QueryRow(ctx,
		`UPDATE accounts SET balance = balance - $1 WHERE id = $2 RETURNING balance`,
		amount, fromAccountID,
	).Scan(&fromBalance); err != nil {
		return TransferResult{}, fmt.Errorf("debit sender: %w", err)
	}
	if err = tx.QueryRow(ctx,
		`UPDATE accounts SET balance = balance + $1 WHERE id = $2 RETURNING balance`,
		amount, toAccountID,
	).Scan(&toBalance); err != nil {
		return TransferResult{}, fmt.Errorf("credit receiver: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return TransferResult{}, fmt.Errorf("commit: %w", err)
	}

	return TransferResult{
		TransactionID: txID,
		FromAccountID: fromAccountID,
		ToAccountID:   toAccountID,
		FromBalance:   fromBalance,
		ToBalance:     toBalance,
	}, nil
}

// replayTransfer returns the result of a transfer that already happened, found
// by its idempotency key, without moving money. The debit line (amount < 0) is
// the sender; the credit line (amount > 0) is the receiver.
func (s *Store) replayTransfer(ctx context.Context, idempotencyKey string) (TransferResult, error) {
	var res TransferResult
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM transactions WHERE idempotency_key = $1`, idempotencyKey,
	).Scan(&res.TransactionID); err != nil {
		return TransferResult{}, fmt.Errorf("replay lookup tx: %w", err)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT account_id FROM entries WHERE transaction_id = $1 AND amount < 0`, res.TransactionID,
	).Scan(&res.FromAccountID); err != nil {
		return TransferResult{}, fmt.Errorf("replay lookup sender: %w", err)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT account_id FROM entries WHERE transaction_id = $1 AND amount > 0`, res.TransactionID,
	).Scan(&res.ToAccountID); err != nil {
		return TransferResult{}, fmt.Errorf("replay lookup receiver: %w", err)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT balance FROM accounts WHERE id = $1`, res.FromAccountID,
	).Scan(&res.FromBalance); err != nil {
		return TransferResult{}, fmt.Errorf("replay sender balance: %w", err)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT balance FROM accounts WHERE id = $1`, res.ToAccountID,
	).Scan(&res.ToBalance); err != nil {
		return TransferResult{}, fmt.Errorf("replay receiver balance: %w", err)
	}
	return res, nil
}
