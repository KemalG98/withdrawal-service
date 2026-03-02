package main

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type WithdrawalStatus string

const (
	StatusPending   WithdrawalStatus = "pending"
	StatusConfirmed WithdrawalStatus = "confirmed"
)

var (
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrIdempotencyConflict = errors.New("idempotency key conflict: different payload")
	ErrInvalidAmount       = errors.New("amount must be greater than zero")
	ErrNotFound            = errors.New("withdrawal not found")
)

type Withdrawal struct {
	ID             uuid.UUID        `json:"id"`
	UserID         uuid.UUID        `json:"user_id"`
	Amount         string           `json:"amount"`
	Currency       string           `json:"currency"`
	Destination    string           `json:"destination"`
	Status         WithdrawalStatus `json:"status"`
	IdempotencyKey string           `json:"idempotency_key"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type CreateWithdrawalRequest struct {
	UserID         uuid.UUID `json:"user_id"`
	Amount         string    `json:"amount"`
	Currency       string    `json:"currency"`
	Destination    string    `json:"destination"`
	IdempotencyKey string    `json:"idempotency_key"`
}
