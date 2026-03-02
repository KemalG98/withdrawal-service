# Withdrawal Service

REST API for creating and managing withdrawal requests with idempotency and concurrency safety.

## Quick Start
```bash
docker compose up --build
```

API is available at `http://localhost:8080`.  
Default token: `secret-token`

## API

### POST /v1/withdrawals
```bash
curl -X POST http://localhost:8080/v1/withdrawals \
  -H "Authorization: Bearer secret-token" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "00000000-0000-0000-0000-000000000001",
    "amount": "100",
    "currency": "USDT",
    "destination": "0xYourWallet",
    "idempotency_key": "unique-key-123"
  }'
```

Status -> Meaning
 201 -> Created successfully |
 400 -> Validation error (amount ≤ 0, missing fields) |
 401 -> Invalid/missing Bearer token |
 409 -> Insufficient balance |
 422 -> Idempotency key reused with different payload |

### GET /v1/withdrawals/{id}
Returns a withdrawal by UUID.

### POST /v1/withdrawals/{id}/confirm
Transitions a `pending` withdrawal to `confirmed`.

## Running Tests
```bash
DATABASE_URL=postgres://postgres:postgres@localhost:5432/withdrawals?sslmode=disable \
  go test ./tests/... -v -count=1
```

## Key Design Decisions

### Concurrency & Double-Spend Prevention

The core protection is **`SELECT ... FOR UPDATE`** on the balance row inside a transaction:
```sql
SELECT amount FROM balances WHERE user_id = $1 FOR UPDATE
```

This acquires a row-level exclusive lock. Any concurrent transaction attempting the same will block until the first commits or rolls back. This ensures:
- Only one transaction reads-then-writes the balance at a time
- No two transactions can both see "sufficient balance" and both deduct

### Idempotency

A `UNIQUE(user_id, idempotency_key)` constraint prevents duplicate rows at the DB level. On conflict:
- If the stored `payload_hash` matches → return the original withdrawal (no-op)
- If hashes differ → return `422 Unprocessable Entity`

The payload hash is computed as `SHA-256(JSON(request))` before the transaction starts.

### Ledger

Every successful withdrawal inserts a `ledger_entries` row with `direction = 'debit'`, creating an auditable append-only record of all balance changes.

### Error Handling

Internal errors are logged with structured `slog` JSON but never leaked to clients. All API errors return `{ "code": "...", "message": "..." }` with no stack traces or DB details.

