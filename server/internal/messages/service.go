package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrForbidden = errors.New("forbidden")
var ErrInvalidInput = errors.New("invalid input")
var ErrNotMember = errors.New("not a member of server")

type MessageService struct {
	repository *MessageRepository
}

func NewMessageService(repository *MessageRepository) *MessageService {
	return &MessageService{
		repository: repository,
	}
}

type CreateMessageInput struct {
	Content string `json:"content"`
}

type MessageCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

type UpdateMessageInput struct {
	Content string `json:"content"`
}

func (s *MessageService) Create(ctx context.Context, channelID int64, userID int64, input *CreateMessageInput) (*Message, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: input cannot be nil", ErrInvalidInput)
	}

	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if channelID <= 0 {
		return nil, fmt.Errorf("%w: invalid channel ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, fmt.Errorf("message repository is nil")
	}

	if strings.TrimSpace(input.Content) == "" {
		return nil, fmt.Errorf("%w: message content cannot be empty", ErrInvalidInput)
	}

	message, err := s.repository.CreateMessage(ctx, channelID, userID, input.Content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("create message: %w", err)
	}

	return message, nil
}

func (s *MessageService) ListMessages(ctx context.Context, channelID int64, userID int64, limit int, before *MessageCursor) ([]Message, bool, error) {
	if userID <= 0 {
		return nil, false, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if channelID <= 0 {
		return nil, false, fmt.Errorf("%w: invalid channel ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, false, fmt.Errorf("message repository is nil")
	}

	const maxMessagePageSize = 100

	if limit > maxMessagePageSize {
		limit = maxMessagePageSize
	}

	if limit <= 0 {
		return nil, false, fmt.Errorf("%w: limit must be positive", ErrInvalidInput)
	}

	if before != nil && (before.ID <= 0 || before.CreatedAt.IsZero()) {
		return nil, false, fmt.Errorf("%w: invalid cursor", ErrInvalidInput)
	}

	messages, hasMore, err := s.repository.ListMessages(ctx, channelID, userID, limit, before)
	if err != nil {
		if errors.Is(err, ErrNotMember) {
			return nil, false, ErrForbidden
		}
		return nil, false, fmt.Errorf("list messages for member: %w", err)
	}

	return messages, hasMore, nil
}

func (s *MessageService) UpdateMessage(ctx context.Context, messageID int64, userID int64, input *UpdateMessageInput) (*Message, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: input cannot be nil", ErrInvalidInput)
	}

	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if messageID <= 0 {
		return nil, fmt.Errorf("%w: invalid message ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, fmt.Errorf("message repository is nil")
	}

	if strings.TrimSpace(input.Content) == "" {
		return nil, fmt.Errorf("%w: message content cannot be empty", ErrInvalidInput)
	}

	message, err := s.repository.UpdateMessage(ctx, messageID, userID, input.Content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("update message: %w", err)
	}

	return message, nil
}

func (s *MessageService) DeleteMessage(ctx context.Context, messageID int64, userID int64) (*Message, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if messageID <= 0 {
		return nil, fmt.Errorf("%w: invalid message ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, fmt.Errorf("message repository is nil")
	}

	message, err := s.repository.DeleteMessage(ctx, messageID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("delete message: %w", err)
	}

	return message, nil
}
