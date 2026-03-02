package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/KemalG98/withdrawal-service/internal/domain"
	"github.com/KemalG98/withdrawal-service/internal/handler"
	"github.com/KemalG98/withdrawal-service/internal/middleware"
	"github.com/KemalG98/withdrawal-service/internal/repository"
	"github.com/KemalG98/withdrawal-service/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

const bearerToken = "test-token"

func newTestServer(t *testing.T) (*chi.Mux, *pgxpool.Pool) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(func() {
		// Reset balance and withdrawals
		pool.Exec(context.Background(), `UPDATE balances SET amount = 1000 WHERE user_id = $1`, testUserID)
		pool.Exec(context.Background(), `DELETE FROM withdrawals WHERE user_id = $1`, testUserID)
		pool.Exec(context.Background(), `DELETE FROM ledger_entries WHERE user_id = $1`, testUserID)
		pool.Close()
	})

	repo := repository.New(pool)
	svc := service.New(repo)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	h := handler.New(svc, logger)

	r := chi.NewRouter()
	r.Use(middleware.BearerAuth(bearerToken))
	r.Post("/v1/withdrawals", h.CreateWithdrawal)
	r.Get("/v1/withdrawals/{id}", h.GetWithdrawal)
	r.Post("/v1/withdrawals/{id}/confirm", h.ConfirmWithdrawal)

	return r, pool
}

func doCreate(t *testing.T, r http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/withdrawals", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// Test 1: Successful withdrawal creation
func TestCreateWithdrawal_Success(t *testing.T) {
	r, _ := newTestServer(t)

	rec := doCreate(t, r, map[string]any{
		"user_id":         testUserID,
		"amount":          "100",
		"currency":        "USDT",
		"destination":     "0xABC",
		"idempotency_key": uuid.NewString(),
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var w domain.Withdrawal
	json.NewDecoder(rec.Body).Decode(&w)
	if w.Status != domain.StatusPending {
		t.Errorf("expected status pending, got %s", w.Status)
	}
}

// Test 2: Insufficient balance → 409
func TestCreateWithdrawal_InsufficientFunds(t *testing.T) {
	r, _ := newTestServer(t)

	rec := doCreate(t, r, map[string]any{
		"user_id":         testUserID,
		"amount":          "99999",
		"currency":        "USDT",
		"destination":     "0xABC",
		"idempotency_key": uuid.NewString(),
	})

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test 3: Idempotency same key same payload → same result
func TestCreateWithdrawal_Idempotency_SamePayload(t *testing.T) {
	r, _ := newTestServer(t)
	key := uuid.NewString()

	body := map[string]any{
		"user_id":         testUserID,
		"amount":          "50",
		"currency":        "USDT",
		"destination":     "0xDEF",
		"idempotency_key": key,
	}

	rec1 := doCreate(t, r, body)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request failed: %d %s", rec1.Code, rec1.Body.String())
	}

	rec2 := doCreate(t, r, body)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("second request failed: %d %s", rec2.Code, rec2.Body.String())
	}

	var w1, w2 domain.Withdrawal
	json.NewDecoder(rec1.Body).Decode(&w1)
	json.Unmarshal([]byte(rec2.Body.String()), &w2) // re-read

	// Re-parse both properly
	json.NewDecoder(bytes.NewBufferString(rec1.Body.String())).Decode(&w1)
	json.NewDecoder(bytes.NewBufferString(rec2.Body.String())).Decode(&w2)

	if w1.ID != w2.ID {
		t.Errorf("expected same withdrawal ID, got %s vs %s", w1.ID, w2.ID)
	}
}

// Test 4: Idempotency same key different payload → 422
func TestCreateWithdrawal_Idempotency_DifferentPayload(t *testing.T) {
	r, _ := newTestServer(t)
	key := uuid.NewString()

	doCreate(t, r, map[string]any{
		"user_id": testUserID, "amount": "10", "currency": "USDT",
		"destination": "0xAAA", "idempotency_key": key,
	})

	rec := doCreate(t, r, map[string]any{
		"user_id": testUserID, "amount": "20", "currency": "USDT",
		"destination": "0xBBB", "idempotency_key": key,
	})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test 5: Amount <= 0 → 400
func TestCreateWithdrawal_InvalidAmount(t *testing.T) {
	r, _ := newTestServer(t)

	for _, amt := range []string{"0", "-1", "abc"} {
		rec := doCreate(t, r, map[string]any{
			"user_id": testUserID, "amount": amt, "currency": "USDT",
			"destination": "0x", "idempotency_key": uuid.NewString(),
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("amount=%s: expected 400, got %d", amt, rec.Code)
		}
	}
}
