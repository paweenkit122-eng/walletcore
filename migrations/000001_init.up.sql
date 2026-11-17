BEGIN;

-- ============================================================================
-- users: people who own a wallet.
-- ============================================================================
CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- accounts: every movement of money is between two accounts.
-- There are user wallet accounts AND internal "system" accounts
-- (e.g. the source of external top-ups). This is the double-entry model:
-- money is never created or destroyed, only moved between accounts.
--
-- `balance` is a materialized total kept in the smallest currency unit
-- (satang for THB). It is always updated inside the SAME transaction as the
-- ledger entries, and is protected by a row lock during transfers so two
-- concurrent transfers can never corrupt it.
-- ============================================================================
CREATE TABLE accounts (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_type TEXT        NOT NULL CHECK (owner_type IN ('user', 'system')),
    owner_id   BIGINT      REFERENCES users(id),  -- NULL for system accounts
    name       TEXT        NOT NULL,              -- e.g. 'wallet', 'topup_source'
    currency   CHAR(3)     NOT NULL DEFAULT 'THB',
    balance    BIGINT      NOT NULL DEFAULT 0,    -- satang
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A user has exactly one wallet per currency.
CREATE UNIQUE INDEX accounts_user_wallet_uniq
    ON accounts (owner_id, currency)
    WHERE owner_type = 'user';

-- ============================================================================
-- transactions: one business event (a top-up or a transfer) that groups
-- two or more ledger entries together.
--
-- `idempotency_key` is UNIQUE: if a client retries the same request (network
-- blip, double tap) with the same key, the duplicate insert fails and we
-- return the original result instead of moving money twice.
-- ============================================================================
CREATE TABLE transactions (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind            TEXT        NOT NULL CHECK (kind IN ('topup', 'transfer')),
    idempotency_key TEXT        NOT NULL UNIQUE,
    reference       TEXT,                           -- optional free-text note
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- entries: the double-entry ledger lines.
-- Convention: amount > 0 is a CREDIT (money into the account),
--             amount < 0 is a DEBIT  (money out of the account).
-- INVARIANT: for every transaction, SUM(amount) over its entries MUST be 0.
-- Entries are immutable (append-only) — corrections are new entries, never
-- edits — which gives a tamper-evident audit trail.
-- ============================================================================
CREATE TABLE entries (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_id BIGINT      NOT NULL REFERENCES transactions(id),
    account_id     BIGINT      NOT NULL REFERENCES accounts(id),
    amount         BIGINT      NOT NULL,            -- satang; +credit / -debit
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX entries_account_id_idx ON entries (account_id, id);

-- Seed the system account that external top-ups flow FROM.
-- Its balance legitimately goes negative: that negative mirrors the total
-- amount of real-world money that has entered the system.
INSERT INTO accounts (owner_type, owner_id, name, currency, balance)
VALUES ('system', NULL, 'topup_source', 'THB', 0);

COMMIT;
