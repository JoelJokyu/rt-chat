package channels

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Channel struct {
	ID        int64     `json:"id"`
	ServerID  int64     `json:"server_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChannelRepository struct {
	db *pgxpool.Pool
}

func NewChannelRepository(db *pgxpool.Pool) *ChannelRepository {
	return &ChannelRepository{
		db: db,
	}
}

func (r *ChannelRepository) CreateChannel(ctx context.Context, serverID int64, userID int64, channelName string) (*Channel, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	channelQuery := `INSERT INTO channels (server_id, name) SELECT $1, $3 WHERE EXISTS (
			SELECT 1
			FROM memberships
			WHERE server_id = $1
			  AND user_id = $2
			  AND role IN ('owner', 'admin')
		)
		RETURNING id, server_id, name, created_at, updated_at`

	channel := &Channel{}
	err := r.db.QueryRow(ctx, channelQuery, serverID, userID, channelName).Scan(&channel.ID, &channel.ServerID, &channel.Name, &channel.CreatedAt, &channel.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return channel, nil
}

func (r *ChannelRepository) ListForMember(ctx context.Context, serverID int64, userID int64) ([]Channel, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	existsQuery := `SELECT EXISTS (SELECT 1 FROM memberships WHERE server_id = $1 AND user_id = $2)`

	var isMember bool
	err := r.db.QueryRow(ctx, existsQuery, serverID, userID).Scan(&isMember)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrNotMember
	}

	channelsQuery := `SELECT c.id, c.server_id, c.name, c.created_at, c.updated_at FROM channels c WHERE c.server_id = $1 AND EXISTS(SELECT 1 FROM memberships m WHERE m.server_id = c.server_id AND m.user_id = $2) ORDER BY c.id`

	rows, err := r.db.Query(ctx, channelsQuery, serverID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channels := make([]Channel, 0)

	for rows.Next() {
		var channel Channel

		err := rows.Scan(&channel.ID, &channel.ServerID, &channel.Name, &channel.CreatedAt, &channel.UpdatedAt)
		if err != nil {
			return nil, err
		}

		channels = append(channels, channel)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return channels, nil
}
