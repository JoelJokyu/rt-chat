package joincodes

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type JoinCodeRepository struct {
	db *pgxpool.Pool
}

func NewJoinCodeRepository(db *pgxpool.Pool) *JoinCodeRepository {
	return &JoinCodeRepository{
		db: db,
	}
}

func (r *JoinCodeRepository) CreateJoinCode(ctx context.Context, serverID int64, userID int64, tokenHash string, expiresAt time.Time) (*JoinCode, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	joincodeQuery := `INSERT INTO join_codes (server_id, created_by, token, expires_at)
						SELECT $1, $2, $3, $4
						WHERE EXISTS (
    						SELECT 1
						    FROM memberships
						    WHERE server_id = $1
 							     AND user_id = $2
							      AND role = 'owner'
						)
					RETURNING id, server_id, created_by, created_at, expires_at;`

	joinCode := &JoinCode{}
	err := r.db.QueryRow(ctx, joincodeQuery, serverID, userID, tokenHash, expiresAt).Scan(&joinCode.ID, &joinCode.ServerID, &joinCode.CreatedBy, &joinCode.CreatedAt, &joinCode.ExpiresAt)
	if err != nil {
		return nil, err
	}

	return joinCode, nil
}

func (r *JoinCodeRepository) JoinServerViaCode(ctx context.Context, userID int64, tokenHash string) (serverID int64, alreadyMember bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	codeQuery := `SELECT server_id
					FROM join_codes
					WHERE token = $1
				  AND expires_at > NOW()
				  AND revoked_at IS NULL
				FOR UPDATE;`

	err = tx.QueryRow(ctx, codeQuery, tokenHash).Scan(&serverID)
	if err != nil {
		return 0, false, err
	}

	membershipQuery := `INSERT INTO memberships (server_id, user_id, role)
						VALUES ($1, $2, 'member')
						ON CONFLICT (server_id, user_id) DO NOTHING`

	tag, err := tx.Exec(ctx, membershipQuery, serverID, userID)
	if err != nil {
		return 0, false, err
	}

	alreadyMember = tag.RowsAffected() == 0

	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}

	return serverID, alreadyMember, nil
}

func (r *JoinCodeRepository) RevokeJoinCode(ctx context.Context, serverID int64, codeID int64, userID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `UPDATE join_codes jc
		SET revoked_at = COALESCE(jc.revoked_at, NOW())
		WHERE jc.id = $1
		  AND jc.server_id = $2
		  AND EXISTS (
		      SELECT 1
		      FROM memberships m
		      WHERE m.server_id = jc.server_id
		        AND m.user_id = $3
		        AND m.role = 'owner'
		  )
		RETURNING jc.id`

	var revokedCodeID int64

	err := r.db.QueryRow(ctx, query, codeID, serverID, userID).Scan(&revokedCodeID)
	if err != nil {
		return err
	}

	return nil
}
