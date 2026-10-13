package joincodes

import (
	"context"
	"errors"
	"fmt"
	"rtchat/internal/auth"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type JoinCodeService struct {
	repository *JoinCodeRepository
}

func NewJoinCodeService(repository *JoinCodeRepository) *JoinCodeService {
	return &JoinCodeService{
		repository: repository,
	}
}

var ErrInvalidInput = errors.New("invalid input")
var ErrForbidden = errors.New("forbidden")

type JoinCode struct {
	ID        int64     `json:"id"`
	ServerID  int64     `json:"server_id"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CreateJoinCodeInput struct {
	ExpiresAt time.Time `json:"expires_at"`
}

type JoinServerInput struct {
	Code string `json:"code"`
}

func (s *JoinCodeService) Create(ctx context.Context, serverID int64, userID int64, input *CreateJoinCodeInput) (*JoinCode, string, error) {
	if input == nil {
		return nil, "", fmt.Errorf("%w: input cannot be nil", ErrInvalidInput)
	}

	if !input.ExpiresAt.After(time.Now()) {
		return nil, "", fmt.Errorf("%w: input should be in the future", ErrInvalidInput)
	}

	if userID <= 0 {
		return nil, "", fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if serverID <= 0 {
		return nil, "", fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, "", fmt.Errorf("join_code repository is nil")
	}

	rawToken, err := auth.GenerateToken()
	if err != nil {
		return nil, "", fmt.Errorf("create join code: %w", err)
	}
	tokenHash := auth.HashToken(rawToken)

	joincode, err := s.repository.CreateJoinCode(ctx, serverID, userID, tokenHash, input.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrForbidden
		}
		return nil, "", fmt.Errorf("save join_code: %w", err)
	}

	return joincode, rawToken, nil
}

func (s *JoinCodeService) Join(ctx context.Context, userID int64, input *JoinServerInput) (serverID int64, alreadyMember bool, err error) {
	if input == nil {
		return 0, false, fmt.Errorf("%w: input cannot be nil", ErrInvalidInput)
	}

	if userID <= 0 {
		return 0, false, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	trimmedCode := strings.TrimSpace(input.Code)
	if trimmedCode == "" {
		return 0, false, fmt.Errorf("%w: code cannot be empty", ErrInvalidInput)
	}

	if s.repository == nil {
		return 0, false, fmt.Errorf("join_code repository is nil")
	}

	tokenHash := auth.HashToken(trimmedCode)

	serverID, alreadyMember, err = s.repository.JoinServerViaCode(ctx, userID, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, ErrInvalidInput
		}
		return 0, false, fmt.Errorf("join server via code: %w", err)
	}

	return serverID, alreadyMember, nil
}

func (s *JoinCodeService) RevokeCode(ctx context.Context, serverID int64, codeID int64, userID int64) error {
	if userID <= 0 || codeID <= 0 || serverID <= 0 {
		return fmt.Errorf("%w: invalid IDs", ErrInvalidInput)
	}

	if s.repository == nil {
		return fmt.Errorf("join_code repository is nil")
	}

	err := s.repository.RevokeJoinCode(ctx, serverID, codeID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		}
		return fmt.Errorf("revoke code: %w", err)
	}

	return nil
}
