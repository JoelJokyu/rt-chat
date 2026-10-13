package servers

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ServerRepository struct {
	db *pgxpool.Pool
}

func NewServerRepository(db *pgxpool.Pool) *ServerRepository {
	return &ServerRepository{
		db: db,
	}
}

func (r *ServerRepository) CreateWithOwner(ctx context.Context, server *Server, ownerID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	//begin, insert sever, inser owner membership, commit

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	serverQuery := `INSERT INTO servers (name) VALUES ($1) RETURNING id, created_at, updated_at`

	err = tx.QueryRow(ctx, serverQuery, server.Name).Scan(&server.ID, &server.CreatedAt, &server.UpdatedAt)
	if err != nil {
		return err
	}

	membershipQuery := `INSERT INTO memberships (server_id, user_id, role) VALUES ($1, $2, 'owner')`

	_, err = tx.Exec(ctx, membershipQuery, server.ID, ownerID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)

}

func (r *ServerRepository) GetByIDForMember(ctx context.Context, serverID int64, userID int64) (*Server, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	serverQuery := `SELECT s.id, s.name, s.created_at, s.updated_at FROM servers s JOIN memberships m ON m.server_id = s.id WHERE s.id = $1 AND m.user_id = $2`

	server := &Server{}
	err := r.db.QueryRow(ctx, serverQuery, serverID, userID).Scan(&server.ID, &server.Name, &server.CreatedAt, &server.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return server, nil
}

func (r *ServerRepository) ListForMember(ctx context.Context, userID int64) ([]Server, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	serversQuery := `SELECT s.id, s.name, s.created_at, s.updated_at FROM servers s JOIN memberships m ON m.server_id = s.id WHERE m.user_id = $1 ORDER BY s.id`

	rows, err := r.db.Query(ctx, serversQuery, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := make([]Server, 0)

	for rows.Next() {
		var server Server

		err := rows.Scan(&server.ID, &server.Name, &server.CreatedAt, &server.UpdatedAt)
		if err != nil {
			return nil, err
		}

		servers = append(servers, server)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return servers, nil
}

func (r *ServerRepository) UpdateName(ctx context.Context, serverID int64, userID int64, name string) (*Server, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	updateQuery := `UPDATE servers AS s
					SET name = $1, updated_at = NOW()
					WHERE s.id = $2
					  AND EXISTS (
					      SELECT 1
					      FROM memberships AS m
					      WHERE m.server_id = s.id
					        AND m.user_id = $3
					        AND m.role = 'owner'
					  )
					RETURNING s.id, s.name, s.created_at, s.updated_at;`

	var updatedServer Server
	err := r.db.QueryRow(ctx, updateQuery, name, serverID, userID).Scan(&updatedServer.ID, &updatedServer.Name, &updatedServer.CreatedAt, &updatedServer.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &updatedServer, nil
}

func (r *ServerRepository) DeleteServer(ctx context.Context, serverID int64, userID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	deleteQuery := `DELETE FROM servers AS s
					WHERE s.id = $1
					  AND EXISTS (
					      SELECT 1
					      FROM memberships AS m
					      WHERE m.server_id = s.id
					        AND m.user_id = $2
					        AND m.role = 'owner'
						  )
					RETURNING s.id;`

	var deletedServerID int64
	err := r.db.QueryRow(ctx, deleteQuery, serverID, userID).Scan(&deletedServerID)
	if err != nil {
		return err
	}

	return nil
}
