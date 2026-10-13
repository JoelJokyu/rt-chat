package memberships

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrNotMember = errors.New("not a member of server")
var ErrInvalidInput = errors.New("invalid input")
var ErrForbidden = errors.New("forbidden")

type MembershipService struct {
	repository *MembershipRepository
}

func NewMembershipService(repository *MembershipRepository) *MembershipService {
	return &MembershipService{
		repository: repository,
	}
}

type MemberCursor struct {
	JoinedAt time.Time `json:"joined_at"`
	UserID   int64     `json:"user_id"`
}

type MemberRoleUpdate struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
}

func (s *MembershipService) ListMembers(ctx context.Context, serverID int64, userID int64, limit int, before *MemberCursor) ([]Member, bool, error) {
	if userID <= 0 {
		return nil, false, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if serverID <= 0 {
		return nil, false, fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, false, fmt.Errorf("membership repository is nil")
	}

	const maxMemberPageSize = 20

	if limit > maxMemberPageSize {
		limit = maxMemberPageSize
	}

	if limit <= 0 {
		return nil, false, fmt.Errorf("%w: limit must be positive", ErrInvalidInput)
	}

	if before != nil && (before.UserID <= 0 || before.JoinedAt.IsZero()) {
		return nil, false, fmt.Errorf("%w: invalid cursor", ErrInvalidInput)
	}

	members, hasMore, err := s.repository.ListMembers(ctx, serverID, userID, limit, before)
	if err != nil {
		if errors.Is(err, ErrNotMember) {
			return nil, false, ErrForbidden
		}
		return nil, false, fmt.Errorf("list members for server: %w", err)
	}

	return members, hasMore, nil
}

func (s *MembershipService) UpdateMemberRole(ctx context.Context, serverID int64, requesterID int64, targetUserID int64, newRole string) (*MemberRoleUpdate, bool, error) {
	if serverID <= 0 {
		return nil, false, fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	if requesterID <= 0 {
		return nil, false, fmt.Errorf("%w: invalid requesting user ID", ErrInvalidInput)
	}

	if targetUserID <= 0 {
		return nil, false, fmt.Errorf("%w: invalid target user ID", ErrInvalidInput)
	}

	if newRole != "member" && newRole != "admin" {
		return nil, false, fmt.Errorf("%w: invalid role", ErrInvalidInput)
	}

	if s.repository == nil {
		return nil, false, fmt.Errorf("membership repository is nil")
	}

	updaterole, updatedOK, err := s.repository.UpdateMemberRole(ctx, serverID, requesterID, targetUserID, newRole)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrForbidden
		}
		return nil, false, fmt.Errorf("update role: %w", err)
	}

	return updaterole, updatedOK, nil
}

func (s *MembershipService) RemoveMember(ctx context.Context, serverID int64, requesterID int64, targetUserID int64) (bool, error) {
	if serverID <= 0 {
		return false, fmt.Errorf("%w: invalid server ID", ErrInvalidInput)
	}

	if requesterID <= 0 {
		return false, fmt.Errorf("%w: invalid requesting user ID", ErrInvalidInput)
	}

	if targetUserID <= 0 {
		return false, fmt.Errorf("%w: invalid target user ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return false, fmt.Errorf("membership repository is nil")
	}

	updatedOK, err := s.repository.RemoveMember(ctx, serverID, requesterID, targetUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrForbidden
		}
		return false, fmt.Errorf("remove member: %w", err)
	}

	return updatedOK, nil
}

func (s *MembershipService) IsMemberOfChannel(ctx context.Context, channelID int64, userID int64) (bool, error) {
	if channelID <= 0 {
		return false, fmt.Errorf("%w: invalid channel ID", ErrInvalidInput)
	}

	if userID <= 0 {
		return false, fmt.Errorf("%w: invalid user ID", ErrInvalidInput)
	}

	if s.repository == nil {
		return false, fmt.Errorf("membership repository is nil")
	}

	return s.repository.IsMemberOfChannel(ctx, channelID, userID)
}
