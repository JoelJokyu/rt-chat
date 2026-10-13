package channels

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrForbidden = errors.New("forbidden")
var ErrNotMember = errors.New("not a member of server")

type ChannelService struct {
	repository *ChannelRepository
}

func NewChannelService(repository *ChannelRepository) *ChannelService {
	return &ChannelService{
		repository: repository,
	}
}

type CreateChannelInput struct {
	Name string `json:"name"`
}

func (s *ChannelService) Create(ctx context.Context, serverID int64, input *CreateChannelInput, userID int64) (*Channel, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: input cannot be nil", ErrInvalidInput)
	}

	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if serverID <= 0 {
		return nil, fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	trimmedName := strings.TrimSpace(input.Name)
	if trimmedName == "" {
		return nil, fmt.Errorf("%w: channel name cannot be empty", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, fmt.Errorf("channel repository is nil")
	}

	channel, err := s.repository.CreateChannel(ctx, serverID, userID, trimmedName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("create channel: %w", err)
	}

	return channel, nil
}

func (s *ChannelService) ListForMember(ctx context.Context, serverID int64, userID int64) ([]Channel, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if serverID <= 0 {
		return nil, fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, fmt.Errorf("channel repository is nil")
	}

	channels, err := s.repository.ListForMember(ctx, serverID, userID)
	if err != nil {
		if errors.Is(err, ErrNotMember) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("list channels for member: %w", err)
	}

	return channels, nil
}
