package service

import (
	"context"

	"github.com/KemalG98/withdrawal-service/internal/domain"
	"github.com/google/uuid"
)

type Repository interface {
	CreateWithdrawal(ctx context.Context, req domain.CreateWithdrawalRequest) (*domain.Withdrawal, error)
	GetWithdrawal(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error)
	ConfirmWithdrawal(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error)
}

type WithdrawalService struct {
	repo Repository
}

func New(repo Repository) *WithdrawalService {
	return &WithdrawalService{repo: repo}
}

func (s *WithdrawalService) Create(ctx context.Context, req domain.CreateWithdrawalRequest) (*domain.Withdrawal, error) {
	return s.repo.CreateWithdrawal(ctx, req)
}

func (s *WithdrawalService) Get(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error) {
	return s.repo.GetWithdrawal(ctx, id)
}

func (s *WithdrawalService) Confirm(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error) {
	return s.repo.ConfirmWithdrawal(ctx, id)
}
