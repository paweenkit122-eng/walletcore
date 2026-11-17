package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store runs wallet/ledger operations against PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wires a Store to the shared connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreateUserWithWallet creates a user and their THB wallet account atomically
// (both, or neither). It returns the new user id and wallet account id.
//
// NOTE: real auth (password hashing + JWT) is Phase 1. For now password_hash is
// stored empty so we can focus on the ledger — do NOT ship this as-is.
func (s *Store) CreateUserWithWallet(ctx context.Context, email string) (userID, accountID int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once we Commit; rolls back on any early return

	if err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, '') RETURNING id`,
		email,
	).Scan(&userID); err != nil {
		return 0, 0, fmt.Errorf("insert user: %w", err)
	}

	if err = tx.QueryRow(ctx,
		`INSERT INTO accounts (owner_type, owner_id, name, currency, balance)
		 VALUES ('user', $1, 'wallet', 'THB', 0) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		return 0, 0, fmt.Errorf("insert wallet: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}
	return userID, accountID, nil
}

// systemAccountID looks up an internal system account by name (e.g. "topup_source")
// inside the given transaction.
func (s *Store) systemAccountID(ctx context.Context, tx pgx.Tx, name string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx,
		`SELECT id FROM accounts WHERE owner_type = 'system' AND name = $1`, name,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("lookup system account %q: %w", name, err)
	}
	return id, nil
}

// TopUpResult is what a successful (or replayed) top-up returns to the caller.
type TopUpResult struct {
	TransactionID int64
	AccountID     int64
	NewBalance    int64 // wallet balance after the top-up, in satang
}

// ErrInvalidAmount is returned when a top-up amount is not positive.
var ErrInvalidAmount = errors.New("amount must be greater than zero")

// TopUp adds `amount` satang into the user wallet `accountID`, funded from the
// internal "topup_source" system account, recorded as ONE double-entry
// transaction. It is idempotent: replaying the same idempotencyKey returns the
// original result without moving money again.
func (s *Store) TopUp(ctx context.Context, accountID int64, amount int64, idempotencyKey string) (TopUpResult, error) {
	// (1) Reject non-positive amounts before touching the database.
	if amount <= 0 {
		return TopUpResult{}, ErrInvalidAmount
	}

	// (2) Open a single DB transaction. Everything below either all commits
	//     together, or none of it does — money is never left half-moved.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TopUpResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // safety net: no-op after a successful Commit

	// (3) Record the business event. idempotency_key is UNIQUE in the DB, so a
	//     replayed request trips a unique violation here instead of inserting
	//     a second transaction.
	var txID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (kind, idempotency_key) VALUES ('topup', $1) RETURNING id`,
		idempotencyKey,
	).Scan(&txID)
	if isUniqueViolation(err) {
		// Seen this key before → DON'T move money again. The failed INSERT has
		// aborted this tx, so roll it back and read the original result on a
		// fresh connection.
		_ = tx.Rollback(ctx)
		return s.replayTopUp(ctx, idempotencyKey)
	}
	if err != nil {
		return TopUpResult{}, fmt.Errorf("insert transaction: %w", err)
	}

	// (4) The two ledger lines of a double entry. Their amounts sum to ZERO:
	//     money leaves topup_source (−amount) and arrives in the wallet (+amount).
	srcID, err := s.systemAccountID(ctx, tx, "topup_source")
	if err != nil {
		return TopUpResult{}, err
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO entries (transaction_id, account_id, amount)
		 VALUES ($1, $2, $3), ($1, $4, $5)`,
		txID, srcID, -amount, accountID, amount,
	); err != nil {
		return TopUpResult{}, fmt.Errorf("insert entries: %w", err)
	}

	// (5) Move the materialized balances. `balance = balance + $1` is evaluated
	//     while Postgres holds the row lock the UPDATE takes, so two concurrent
	//     top-ups to the same wallet can't lose an update.
	if _, err = tx.Exec(ctx,
		`UPDATE accounts SET balance = balance - $1 WHERE id = $2`,
		amount, srcID,
	); err != nil {
		return TopUpResult{}, fmt.Errorf("debit source: %w", err)
	}

	var newBalance int64
	if err = tx.QueryRow(ctx,
		`UPDATE accounts SET balance = balance + $1 WHERE id = $2 RETURNING balance`,
		amount, accountID,
	).Scan(&newBalance); err != nil {
		// e.g. the wallet id doesn't exist (no row to update).
		return TopUpResult{}, fmt.Errorf("credit wallet: %w", err)
	}

	// (6) Commit: now everything becomes visible, all at once.
	if err = tx.Commit(ctx); err != nil {
		return TopUpResult{}, fmt.Errorf("commit: %w", err)
	}

	return TopUpResult{TransactionID: txID, AccountID: accountID, NewBalance: newBalance}, nil
}

// replayTopUp returns the result of a top-up that already happened, found by its
// idempotency key — without moving any money. It reads the credit (wallet) line
// of the original transaction and that wallet's current balance.
//
// Tradeoff: NewBalance here is the wallet's *current* balance, not necessarily
// the balance at the moment of the original top-up (later operations may have
// changed it). A stricter design stores the original response and returns it
// verbatim; this is good enough for Phase 2.
func (s *Store) replayTopUp(ctx context.Context, idempotencyKey string) (TopUpResult, error) {
	var (
		txID      int64
		accountID int64
		balance   int64
	)
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, e.account_id, a.balance
		FROM transactions t
		JOIN entries  e ON e.transaction_id = t.id AND e.amount > 0
		JOIN accounts a ON a.id = e.account_id
		WHERE t.idempotency_key = $1`,
		idempotencyKey,
	).Scan(&txID, &accountID, &balance)
	if err != nil {
		return TopUpResult{}, fmt.Errorf("replay lookup: %w", err)
	}
	return TopUpResult{TransactionID: txID, AccountID: accountID, NewBalance: balance}, nil
}

// isUniqueViolation reports whether err is a Postgres UNIQUE-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
