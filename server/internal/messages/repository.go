package messages

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Message struct {
	ID        int64      `json:"id"`
	ChannelID int64      `json:"channel_id"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
	AuthorID  int64      `json:"author_id"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type MessageRepository struct {
	db *pgxpool.Pool
}

func NewMessageRepository(db *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{
		db: db,
	}
}

func (r *MessageRepository) CreateMessage(ctx context.Context, channelID int64, userID int64, content string) (*Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	messageQuery := `INSERT INTO messages (channel_id, author_id, content) SELECT $1, $2, $3 WHERE EXISTS (
			SELECT 1
			FROM channels c
			JOIN memberships m ON m.server_id = c.server_id
			WHERE c.id = $1
			  AND m.user_id = $2
		)
		RETURNING id, channel_id, author_id, content, created_at`

	message := &Message{}

	err := r.db.QueryRow(ctx, messageQuery, channelID, userID, content).Scan(&message.ID, &message.ChannelID, &message.AuthorID, &message.Content, &message.CreatedAt)
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (r *MessageRepository) ListMessages(ctx context.Context, channelID int64, userID int64, limit int, before *MessageCursor) ([]Message, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var beforeCreatedAt any
	var beforeID any

	if before != nil {
		beforeCreatedAt = before.CreatedAt
		beforeID = before.ID
	}

	fetchLimit := limit + 1

	query := `
		WITH access AS MATERIALIZED (
			SELECT EXISTS (
				SELECT 1
				FROM channels AS c
				JOIN memberships AS m ON m.server_id = c.server_id
				WHERE c.id = $1
				  AND m.user_id = $2
			) AS is_member
		)
		SELECT
			access.is_member,
			msg.id,
			msg.channel_id,
			msg.author_id,
			msg.content,
			msg.created_at
		FROM access
		LEFT JOIN LATERAL (
			SELECT id, channel_id, author_id, content, created_at
			FROM messages
			WHERE access.is_member
			  AND channel_id = $1
			  AND deleted_at IS NULL
			  AND (
				  $3::timestamptz IS NULL
				  OR (created_at, id) < ($3, $4)
			  )
			ORDER BY created_at DESC, id DESC
			LIMIT $5
		) AS msg ON true
		ORDER BY msg.created_at DESC NULLS LAST, msg.id DESC NULLS LAST;
	`

	rows, err := r.db.Query(ctx, query, channelID, userID, beforeCreatedAt, beforeID, fetchLimit)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	messages := make([]Message, 0)

	for rows.Next() {
		var isMember bool
		var id pgtype.Int8
		var rowChannelID pgtype.Int8
		var authorID pgtype.Int8
		var content pgtype.Text
		var createdAt pgtype.Timestamptz

		err := rows.Scan(&isMember, &id, &rowChannelID, &authorID, &content, &createdAt)
		if err != nil {
			return nil, false, err
		}

		if !isMember {
			return nil, false, ErrNotMember
		}

		// LEFT JOIN returns one row with NULL message fields when the page is empty.
		if !id.Valid {
			continue
		}

		messages = append(messages, Message{
			ID:        id.Int64,
			ChannelID: rowChannelID.Int64,
			AuthorID:  authorID.Int64,
			Content:   content.String,
			CreatedAt: createdAt.Time,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	return messages, hasMore, nil
}

func (r *MessageRepository) UpdateMessage(ctx context.Context, messageID int64, userID int64, content string) (*Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	updateQuery := `UPDATE messages
					SET content = $1, updated_at = NOW()
					WHERE id = $2
					  AND author_id = $3
					  AND deleted_at IS NULL
					RETURNING id, channel_id, author_id, content, created_at, updated_at;`

	message := &Message{}

	err := r.db.QueryRow(ctx, updateQuery, content, messageID, userID).Scan(&message.ID, &message.ChannelID, &message.AuthorID, &message.Content, &message.CreatedAt, &message.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (r *MessageRepository) DeleteMessage(ctx context.Context, messageID int64, userID int64) (*Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	deleteQuery := `UPDATE messages AS msg
					SET deleted_at = NOW()
					WHERE msg.id = $1
					  AND msg.deleted_at IS NULL
					  AND (
					      msg.author_id = $2
					      OR EXISTS (
					          SELECT 1
					          FROM channels AS c
					          JOIN memberships AS m ON m.server_id = c.server_id
					          WHERE c.id = msg.channel_id
					            AND m.user_id = $2
					            AND m.role IN ('admin', 'owner')
					      )
					  )
					RETURNING msg.id, msg.channel_id;`

	message := &Message{}

	err := r.db.QueryRow(ctx, deleteQuery, messageID, userID).Scan(&message.ID, &message.ChannelID)
	if err != nil {
		return nil, err
	}

	return message, nil
}
