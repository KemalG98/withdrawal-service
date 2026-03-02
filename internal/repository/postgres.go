package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KemalG98/withdrawal-service/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type PostgresRepo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *PostgresRepo {
	return &PostgresRepo{pool: pool}
}

func payloadHash(req domain.CreateWithdrawalRequest) string {
	b, _ := json.Marshal(req)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// CreateWithdrawal executes the full withdrawal creation atomically:
//  1. Locks the balance row with SELECT FOR UPDATE to prevent concurrent double-spend
//  2. Checks sufficient balance
//  3. Deducts balance
//  4. Inserts withdrawal record
//  5. Inserts ledger entry
//
// Idempotency is enforced via UNIQUE(user_id, idempotency_key).
// On conflict we compare payload hashes — same payload returns existing row, different payload returns ErrIdempotencyConflict.
func (r *PostgresRepo) CreateWithdrawal(ctx context.Context, req domain.CreateWithdrawalRequest) (*domain.Withdrawal, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || !amount.IsPositive() {
		return nil, domain.ErrInvalidAmount
	}

	hash := payloadHash(req)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock balance row — prevents concurrent transactions from reading stale balance
	var balance decimal.Decimal
	err = tx.QueryRow(ctx,
		`SELECT amount FROM balances WHERE user_id = $1 FOR UPDATE`,
		req.UserID,
	).Scan(&balance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInsufficientFunds
		}
		return nil, fmt.Errorf("lock balance: %w", err)
	}

	if balance.LessThan(amount) {
		return nil, domain.ErrInsufficientFunds
	}

	// Deduct balance
	_, err = tx.Exec(ctx,
		`UPDATE balances SET amount = amount - $1, updated_at = now() WHERE user_id = $2`,
		amount, req.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("deduct balance: %w", err)
	}

	// Insert withdrawal with idempotency guard
	var w domain.Withdrawal
	err = tx.QueryRow(ctx, `
        INSERT INTO withdrawals (user_id, amount, currency, destination, idempotency_key, payload_hash)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id, user_id, amount, currency, destination, status, idempotency_key, created_at, updated_at`,
		req.UserID, amount, req.Currency, req.Destination, req.IdempotencyKey, hash,
	).Scan(&w.ID, &w.UserID, &w.Amount, &w.Currency, &w.Destination, &w.Status, &w.IdempotencyKey, &w.CreatedAt, &w.UpdatedAt)

	if err != nil {
		// Unique constraint violation on idempotency key
		if isUniqueViolation(err) {
			tx.Rollback(ctx)
			return r.handleIdempotency(ctx, req, hash)
		}
		return nil, fmt.Errorf("insert withdrawal: %w", err)
	}

	// Ledger entry
	_, err = tx.Exec(ctx,
		`INSERT INTO ledger_entries (user_id, withdrawal_id, amount, direction) VALUES ($1, $2, $3, 'debit')`,
		req.UserID, w.ID, amount,
	)
	if err != nil {
		return nil, fmt.Errorf("insert ledger: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &w, nil
}

// handleIdempotency checks if the existing record matches the incoming payload hash.
func (r *PostgresRepo) handleIdempotency(ctx context.Context, req domain.CreateWithdrawalRequest, hash string) (*domain.Withdrawal, error) {
	var w domain.Withdrawal
	var storedHash string
	err := r.pool.QueryRow(ctx, `
        SELECT id, user_id, amount, currency, destination, status, idempotency_key, payload_hash, created_at, updated_at
        FROM withdrawals
        WHERE user_id = $1 AND idempotency_key = $2`,
		req.UserID, req.IdempotencyKey,
	).Scan(&w.ID, &w.UserID, &w.Amount, &w.Currency, &w.Destination, &w.Status, &w.IdempotencyKey, &storedHash, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("idempotency lookup: %w", err)
	}
	if storedHash != hash {
		return nil, domain.ErrIdempotencyConflict
	}
	return &w, nil
}

func (r *PostgresRepo) GetWithdrawal(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error) {
	var w domain.Withdrawal
	err := r.pool.QueryRow(ctx, `
        SELECT id, user_id, amount, currency, destination, status, idempotency_key, created_at, updated_at
        FROM withdrawals WHERE id = $1`, id,
	).Scan(&w.ID, &w.UserID, &w.Amount, &w.Currency, &w.Destination, &w.Status, &w.IdempotencyKey, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get withdrawal: %w", err)
	}
	return &w, nil
}

func (r *PostgresRepo) ConfirmWithdrawal(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error) {
	var w domain.Withdrawal
	err := r.pool.QueryRow(ctx, `
        UPDATE withdrawals SET status = 'confirmed', updated_at = now()
        WHERE id = $1 AND status = 'pending'
        RETURNING id, user_id, amount, currency, destination, status, idempotency_key, created_at, updated_at`,
		id,
	).Scan(&w.ID, &w.UserID, &w.Amount, &w.Currency, &w.Destination, &w.Status, &w.IdempotencyKey, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("confirm withdrawal: %w", err)
	}
	return &w, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && (contains(err.Error(), "23505") || contains(err.Error(), "unique"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
