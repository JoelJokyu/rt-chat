package auth

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Session struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	TokenHash string     `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

type SessionRepository struct {
	db *pgxpool.Pool
}

func NewSessionRepository(db *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{
		db: db,
	}
}

func (r *SessionRepository) Insert(ctx context.Context, session *Session) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `INSERT INTO sessions (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at, expires_at`

	return r.db.QueryRow(ctx, query, session.UserID, session.TokenHash, session.ExpiresAt).Scan(
		&session.ID,
		&session.CreatedAt,
		&session.ExpiresAt,
	)

}

func (r *SessionRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `SELECT id, user_id, token_hash, created_at, expires_at, revoked_at FROM sessions WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()`

	session := &Session{}
	err := r.db.QueryRow(ctx, query, tokenHash).Scan(
		&session.ID,
		&session.UserID,
		&session.TokenHash,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.RevokedAt,
	)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (r *SessionRepository) Revoke(ctx context.Context, sessionID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `UPDATE sessions SET revoked_at = NOW()
		 WHERE id = $1
		 AND revoked_at IS NULL`

	_, err := r.db.Exec(ctx, query, sessionID)
	return err
}
