package tests

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/KemalG98/withdrawal-service/internal/domain"
	"github.com/google/uuid"
)

// Test 6: Concurrent withdrawals only those that fit the balance should succeed
func TestCreateWithdrawal_Concurrent(t *testing.T) {
	r, pool := newTestServer(t)
	_ = pool

	// Balance is 1000 USDT, send 10 concurrent requests of 200 each (total 2000)
	// Only 5 should succeed
	const (
		goroutines   = 10
		amountEach   = "200"
		balanceTotal = 1000
	)

	type result struct {
		status int
		body   string
	}

	results := make([]result, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := doCreate(t, r, map[string]any{
				"user_id":         testUserID,
				"amount":          amountEach,
				"currency":        "USDT",
				"destination":     "0xCONCURRENT",
				"idempotency_key": uuid.NewString(), // unique per request
			})
			results[i] = result{status: rec.Code, body: rec.Body.String()}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, res := range results {
		if res.status == 201 {
			successCount++
			var w domain.Withdrawal
			json.Unmarshal([]byte(res.body), &w)
			if w.Status != domain.StatusPending {
				t.Errorf("unexpected status: %s", w.Status)
			}
		} else if res.status != 409 {
			t.Errorf("expected 201 or 409, got %d: %s", res.status, res.body)
		}
	}

	expected := balanceTotal / 200
	if successCount != expected {
		t.Errorf("expected %d successful withdrawals, got %d", expected, successCount)
	}
}
