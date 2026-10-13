package servers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrServerNotFound = errors.New("server not found")
var ErrForbidden = errors.New("forbidden")

type ServerService struct {
	repository *ServerRepository
}

type CreateServerInput struct {
	Name string `json:"name"`
}

type UpdateServerNameInput struct {
	Name string `json:"name"`
}

func NewServerService(repository *ServerRepository) *ServerService {
	return &ServerService{
		repository: repository,
	}
}

func (s *ServerService) Create(ctx context.Context, input *CreateServerInput, ownerID int64) (*Server, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: input cannot be nil", ErrInvalidInput)
	}

	if ownerID <= 0 {
		return nil, fmt.Errorf("%w: invalid owner ID", ErrInvalidInput)
	}

	trimmedName := strings.TrimSpace(input.Name)
	if trimmedName == "" {
		return nil, fmt.Errorf("%w: server name cannot be empty", ErrInvalidInput)
	}

	server := &Server{
		Name: trimmedName,
	}

	if s.repository == nil {
		return nil, fmt.Errorf("repository is nil")
	}

	err := s.repository.CreateWithOwner(ctx, server, ownerID)
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}

	return server, nil
}

func (s *ServerService) GetByIDForMember(ctx context.Context, serverID int64, userID int64) (*Server, error) {
	if serverID <= 0 {
		return nil, fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, fmt.Errorf("server repository is nil")
	}

	server, err := s.repository.GetByIDForMember(ctx, serverID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("get server: %w", err)
	}

	return server, nil
}

func (s *ServerService) ListForMember(ctx context.Context, userID int64) ([]Server, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, fmt.Errorf("server repository is nil")
	}

	servers, err := s.repository.ListForMember(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list servers for member: %w", err)
	}

	return servers, nil
}

func (s *ServerService) UpdateName(ctx context.Context, serverID int64, userID int64, input *UpdateServerNameInput) (*Server, error) {
	if input == nil {
		return nil, fmt.Errorf("%w: input cannot be nil", ErrInvalidInput)
	}

	if serverID <= 0 {
		return nil, fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	if userID <= 0 {
		return nil, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	trimmedName := strings.TrimSpace(input.Name)
	if trimmedName == "" {
		return nil, fmt.Errorf("%w: server name cannot be empty", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, fmt.Errorf("server repository is nil")
	}

	server, err := s.repository.UpdateName(ctx, serverID, userID, trimmedName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("update server name: %w", err)
	}

	return server, nil
}

func (s *ServerService) DeleteServer(ctx context.Context, serverID int64, userID int64) error {
	if serverID <= 0 {
		return fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	if userID <= 0 {
		return fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return fmt.Errorf("server repository is nil")
	}

	if err := s.repository.DeleteServer(ctx, serverID, userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrForbidden
		}
		return fmt.Errorf("delete server: %w", err)
	}

	return nil
}
