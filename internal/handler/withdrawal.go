package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/KemalG98/withdrawal-service/internal/domain"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service interface {
	Create(ctx context.Context, req domain.CreateWithdrawalRequest) (*domain.Withdrawal, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error)
	Confirm(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error)
}

type Handler struct {
	svc    Service
	logger *slog.Logger
}

func New(svc Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

type createRequest struct {
	UserID         uuid.UUID `json:"user_id"`
	Amount         string    `json:"amount"`
	Currency       string    `json:"currency"`
	Destination    string    `json:"destination"`
	IdempotencyKey string    `json:"idempotency_key"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{Code: code, Message: msg})
}

func (h *Handler) CreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}

	// Validation
	if req.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "idempotency_key is required")
		return
	}
	if uuid.Nil == req.UserID {
		writeError(w, http.StatusBadRequest, "validation_error", "user_id is required")
		return
	}
	if req.Destination == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "destination is required")
		return
	}
	if req.Currency != "USDT" {
		writeError(w, http.StatusBadRequest, "validation_error", "only USDT is supported")
		return
	}
	amt, err := decimal.NewFromString(req.Amount)
	if err != nil || !amt.IsPositive() {
		writeError(w, http.StatusBadRequest, "validation_error", "amount must be greater than 0")
		return
	}

	domainReq := domain.CreateWithdrawalRequest{
		UserID:         req.UserID,
		Amount:         req.Amount,
		Currency:       req.Currency,
		Destination:    req.Destination,
		IdempotencyKey: req.IdempotencyKey,
	}

	withdrawal, err := h.svc.Create(r.Context(), domainReq)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidAmount):
			writeError(w, http.StatusBadRequest, "invalid_amount", "amount must be greater than 0")
		case errors.Is(err, domain.ErrInsufficientFunds):
			h.logger.Info("insufficient funds", "user_id", req.UserID)
			writeError(w, http.StatusConflict, "insufficient_funds", "insufficient balance")
		case errors.Is(err, domain.ErrIdempotencyConflict):
			writeError(w, http.StatusUnprocessableEntity, "idempotency_conflict", "idempotency key used with different payload")
		default:
			h.logger.Error("create withdrawal failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		}
		return
	}

	h.logger.Info("withdrawal created", "id", withdrawal.ID, "user_id", withdrawal.UserID, "amount", withdrawal.Amount)
	writeJSON(w, http.StatusCreated, withdrawal)
}

func (h *Handler) GetWithdrawal(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid withdrawal id")
		return
	}

	withdrawal, err := h.svc.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "withdrawal not found")
			return
		}
		h.logger.Error("get withdrawal failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}

	writeJSON(w, http.StatusOK, withdrawal)
}

func (h *Handler) ConfirmWithdrawal(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid withdrawal id")
		return
	}

	withdrawal, err := h.svc.Confirm(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "withdrawal not found or already confirmed")
			return
		}
		h.logger.Error("confirm withdrawal failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}

	h.logger.Info("withdrawal confirmed", "id", withdrawal.ID)
	writeJSON(w, http.StatusOK, withdrawal)
}
