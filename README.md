# WalletCore — Digital Wallet Backend (Go)

A production-minded **digital wallet / payments backend** built around a
**double-entry ledger**. It demonstrates the hard parts real fintech systems
care about: **money that can never be lost or created**, **idempotent transfers**,
and **concurrency-safe balance updates** under load.

> พอร์ตโฟลิโอโปรเจกต์สำหรับฝึกงานสหกิจ สาย Backend / Systems — เน้นความถูกต้องของเงิน
> (double-entry), กันโอนซ้ำ (idempotency), และกัน race condition ตอนโอนพร้อมกัน

[![Go](https://img.shields.io/badge/Go-1.23-00ADD8)](https://go.dev)
[![Postgres](https://img.shields.io/badge/Postgres-16-336791)](https://www.postgresql.org)
[![Status](https://img.shields.io/badge/status-Phase%200%20(scaffold)-yellow)](#️-roadmap)

> **📍 Status — Phase 0 (scaffold).** What runs today: Dockerized Postgres,
> schema migrations, config, a pgx connection pool, and `/healthz` · `/readyz`
> endpoints. The table below is the **target design**; the money-movement logic
> (top-up, transfer, idempotency, row-locking) is being built in Phases 1–5 —
> see the [Roadmap](#️-roadmap). Documenting the design before the code is deliberate.

---

## ✨ Why this design (the interesting parts)

> The behaviours below are the **design goals** this schema is built for. The
> ledger schema (`migrations/`) already encodes them; the HTTP handlers that
> enforce them land in Phases 2–3.

| Concern | How WalletCore handles it |
|---|---|
| **Money integrity** | Double-entry ledger: every transaction has entries that **sum to zero**. Money is only ever *moved*, never created. |
| **No double-spend on retry** | Every money-moving request carries an **idempotency key** (UNIQUE in DB). A retried request returns the original result instead of moving money twice. |
| **Concurrent transfers** | Balances are updated inside a single DB transaction with a **row lock** (`SELECT … FOR UPDATE`), so two simultaneous transfers can't corrupt a balance. |
| **No negative balance** | A debit is rejected inside the locked transaction if funds are insufficient. |
| **Auditability** | Ledger `entries` are **append-only** (immutable) — corrections are new entries, giving a tamper-evident trail. |

Amounts are stored as integers in the smallest unit (**satang** for THB) to avoid
floating-point money bugs.

---

## 🏗️ Data model

```
users ──1:N──> accounts ──1:N──> entries <──N:1── transactions
                                   (sum per transaction = 0)
```

- **users** — wallet owners
- **accounts** — user wallets + internal `system` accounts (e.g. `topup_source`)
- **transactions** — one business event (`topup` / `transfer`) + unique `idempotency_key`
- **entries** — double-entry lines (`+credit` / `-debit`), append-only

Schema: [`migrations/000001_init.up.sql`](migrations/000001_init.up.sql)

---

## 🧰 Tech stack

- **Go 1.23** · **chi** (router) · **pgx** (Postgres driver/pool)
- **PostgreSQL 16** · **golang-migrate** (migrations)
- **Docker Compose** · **GitHub Actions** (CI — planned)
- Structured logging with `log/slog`, graceful shutdown

---

## 🚀 Run it

**No Go needed** — everything runs in Docker:

```bash
docker compose up --build
```

This starts Postgres, applies migrations, then starts the API on `:8080`.

Check it's alive:

```bash
curl localhost:8080/healthz      # {"status":"ok"}
curl localhost:8080/readyz       # {"status":"ready"}   (pings the DB)
curl localhost:8080/api/v1/ping  # {"message":"pong"}
```

Handy commands (see `make help`):

```bash
make up          # start the full stack
make logs        # tail api logs
make psql        # open a psql shell
make down-v      # stop and wipe data
```

### Developing locally (optional)

Install Go to get fast iteration + IDE support:

```bash
brew install go      # macOS
make tidy            # sync go.mod / go.sum
make run             # run api locally against the compose Postgres
```

---

## 📁 Project structure

```
walletcore/
├── cmd/api/            # main entrypoint
├── internal/
│   ├── config/         # env-based configuration
│   ├── database/       # pgx connection pool
│   └── server/         # router, middleware, handlers
├── migrations/         # SQL migrations (golang-migrate)
├── docker-compose.yml  # db + migrate + api
├── Dockerfile          # multi-stage build
└── Makefile
```

---

## 🗺️ Roadmap

- [x] **Phase 0** — Scaffold: Docker Compose, Postgres, migrations, health checks
- [ ] **Phase 1** — Auth: register / login, JWT
- [ ] **Phase 2** — Ledger core: accounts + double-entry entries + top-up
- [ ] **Phase 3** — Transfer: DB transaction + row lock + idempotency
- [ ] **Phase 4** — Query: balance + statement (pagination)
- [ ] **Phase 5** — Correctness: concurrency load tests + ledger-invariant tests
- [ ] **Phase 6** — Ship: OpenAPI/Swagger docs + deploy (Fly.io / Render / Railway)
- [ ] **Stretch** — Redis idempotency cache, rate limiting, admin reconciliation report
