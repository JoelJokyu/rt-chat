package users

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

type User struct {
	ID           int64     `json:"id"`
	DisplayName  string    `json:"display_name"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (r *UserRepository) Insert(ctx context.Context, user *User) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `INSERT INTO users (email, password_hash, display_name) 
         VALUES ($1, $2, $3) 
         RETURNING id, created_at, updated_at`

	return r.db.QueryRow(ctx, query, user.Email, user.PasswordHash, user.DisplayName).Scan(
		&user.ID,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `SELECT id, display_name, email, password_hash, created_at, updated_at FROM users WHERE email = $1`

	user := &User{}
	err := r.db.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.DisplayName,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return user, nil
}
