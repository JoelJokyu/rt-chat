package memberships

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Member struct {
	UserID      int64     `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
}

type MembershipRepository struct {
	db *pgxpool.Pool
}

func NewMembershipRepository(db *pgxpool.Pool) *MembershipRepository {
	return &MembershipRepository{
		db: db,
	}
}

func (r *MembershipRepository) ListMembers(ctx context.Context, serverID int64, userID int64, limit int, before *MemberCursor) ([]Member, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	existsQuery := `SELECT EXISTS (SELECT 1 FROM memberships WHERE server_id = $1 AND user_id = $2)`

	var isMember bool
	if err := r.db.QueryRow(ctx, existsQuery, serverID, userID).Scan(&isMember); err != nil {
		return nil, false, err
	}
	if !isMember {
		return nil, false, ErrNotMember
	}

	var beforeJoinedAt any
	var beforeUserID any

	if before != nil {
		beforeJoinedAt = before.JoinedAt
		beforeUserID = before.UserID
	}

	fetchLimit := limit + 1

	membersQuery := `SELECT u.id, u.display_name, m.role, m.joined_at
						FROM memberships AS m
						JOIN users AS u ON u.id = m.user_id
						WHERE m.server_id = $1
						  AND (
						      $2::timestamptz IS NULL
							  OR (m.joined_at, m.user_id) < ($2, $3)
						  )
						ORDER BY m.joined_at DESC, m.user_id DESC
						LIMIT $4;`

	rows, err := r.db.Query(ctx, membersQuery, serverID, beforeJoinedAt, beforeUserID, fetchLimit)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	members := make([]Member, 0)

	for rows.Next() {
		var member Member

		err := rows.Scan(&member.UserID, &member.DisplayName, &member.Role, &member.JoinedAt)
		if err != nil {
			return nil, false, err
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}

	return members, hasMore, nil
}

func (r *MembershipRepository) UpdateMemberRole(ctx context.Context, serverID int64, requesterID int64, targetUserID int64, newRole string) (member *MemberRoleUpdate, updatedOK bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `UPDATE memberships AS target
				SET role = $4
				WHERE target.server_id = $1
				  AND target.user_id = $3
				  AND target.role <> 'owner'
				  AND $4 IN ('member', 'admin')
				  AND EXISTS (
				      SELECT 1
				      FROM memberships AS requester
				      WHERE requester.server_id = $1
				        AND requester.user_id = $2
				        AND requester.role = 'owner'
				  )
				RETURNING target.user_id, target.role;`

	updated := &MemberRoleUpdate{}

	err = r.db.QueryRow(ctx, query, serverID, requesterID, targetUserID, newRole).Scan(&updated.UserID, &updated.Role)
	if err != nil {
		return nil, false, err
	}

	return updated, true, nil
}

func (r *MembershipRepository) RemoveMember(ctx context.Context, serverID int64, requesterID int64, targetUserID int64) (updatedOK bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	removeQuery := `DELETE FROM memberships AS target
					WHERE target.server_id = $1
					  AND target.user_id = $3
					  AND target.role = 'member'
					  AND EXISTS (
					      SELECT 1
					      FROM memberships AS requester
					      WHERE requester.server_id = $1
					        AND requester.user_id = $2
					        AND requester.role IN ('owner', 'admin')
					  )
					RETURNING target.user_id;`

	var removedUserID int64

	err = r.db.QueryRow(ctx, removeQuery, serverID, requesterID, targetUserID).Scan(&removedUserID)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *MembershipRepository) IsMemberOfChannel(ctx context.Context, channelID int64, userID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	checkQuery := `SELECT EXISTS (
					    SELECT 1
					    FROM channels AS c
					    JOIN memberships AS m ON m.server_id = c.server_id
					    WHERE c.id = $1 AND m.user_id = $2
					);`

	var isMember bool
	err := r.db.QueryRow(ctx, checkQuery, channelID, userID).Scan(&isMember)
	if err != nil {
		return false, err
	}
	return isMember, nil
}
