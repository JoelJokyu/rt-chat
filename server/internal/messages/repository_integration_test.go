package messages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMessageLifecycleAndPermissions(t *testing.T) {
	pool := openMessageTestPool(t)
	ctx := context.Background()

	var serverID int64
	userIDs := make([]int64, 0, 5)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if serverID != 0 {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverID); err != nil {
				t.Errorf("clean up test server: %v", err)
			}
		}

		for _, userID := range userIDs {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userID); err != nil {
				t.Errorf("clean up test user %d: %v", userID, err)
			}
		}
	})

	ownerID := createMessageTestUser(t, ctx, pool, "Owner")
	userIDs = append(userIDs, ownerID)
	adminID := createMessageTestUser(t, ctx, pool, "Admin")
	userIDs = append(userIDs, adminID)
	authorID := createMessageTestUser(t, ctx, pool, "Author")
	userIDs = append(userIDs, authorID)
	memberID := createMessageTestUser(t, ctx, pool, "Member")
	userIDs = append(userIDs, memberID)
	outsiderID := createMessageTestUser(t, ctx, pool, "Outsider")
	userIDs = append(userIDs, outsiderID)

	serverName := fmt.Sprintf("message-test-%d", time.Now().UnixNano())
	if err := pool.QueryRow(ctx,
		"INSERT INTO servers (name) VALUES ($1) RETURNING id",
		serverName,
	).Scan(&serverID); err != nil {
		t.Fatalf("create test server: %v", err)
	}

	for _, membership := range []struct {
		userID int64
		role   string
	}{
		{userID: ownerID, role: "owner"},
		{userID: adminID, role: "admin"},
		{userID: authorID, role: "member"},
		{userID: memberID, role: "member"},
	} {
		_, err := pool.Exec(ctx,
			"INSERT INTO memberships (server_id, user_id, role) VALUES ($1, $2, $3)",
			serverID,
			membership.userID,
			membership.role,
		)
		if err != nil {
			t.Fatalf("create %s membership: %v", membership.role, err)
		}
	}

	var channelID int64
	if err := pool.QueryRow(ctx,
		"INSERT INTO channels (server_id, name) VALUES ($1, $2) RETURNING id",
		serverID,
		"general",
	).Scan(&channelID); err != nil {
		t.Fatalf("create test channel: %v", err)
	}

	service := NewMessageService(NewMessageRepository(pool))

	first, err := service.Create(ctx, channelID, authorID, &CreateMessageInput{Content: "first message"})
	if err != nil {
		t.Fatalf("author creates message: %v", err)
	}
	if first.ID <= 0 || first.ChannelID != channelID || first.AuthorID != authorID {
		t.Fatalf("created message has unexpected fields: %+v", first)
	}

	if _, err := service.Create(ctx, channelID, outsiderID, &CreateMessageInput{Content: "not allowed"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("nonmember create error = %v; want ErrForbidden", err)
	}

	if _, err := service.UpdateMessage(ctx, first.ID, adminID, &UpdateMessageInput{Content: "admin edit"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin editing another user's message error = %v; want ErrForbidden", err)
	}

	updated, err := service.UpdateMessage(ctx, first.ID, authorID, &UpdateMessageInput{Content: "edited message"})
	if err != nil {
		t.Fatalf("author edits own message: %v", err)
	}
	if updated.Content != "edited message" || updated.UpdatedAt == nil {
		t.Fatalf("updated message has unexpected fields: %+v", updated)
	}

	second, err := service.Create(ctx, channelID, authorID, &CreateMessageInput{Content: "second message"})
	if err != nil {
		t.Fatalf("create second message: %v", err)
	}
	third, err := service.Create(ctx, channelID, authorID, &CreateMessageInput{Content: "third message"})
	if err != nil {
		t.Fatalf("create third message: %v", err)
	}

	if _, err := service.DeleteMessage(ctx, first.ID, memberID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("regular member deleting another user's message error = %v; want ErrForbidden", err)
	}

	deletedByAdmin, err := service.DeleteMessage(ctx, first.ID, adminID)
	if err != nil {
		t.Fatalf("admin deletes message: %v", err)
	}
	if deletedByAdmin.ID != first.ID || deletedByAdmin.ChannelID != channelID {
		t.Fatalf("delete returned wrong message routing data: %+v", deletedByAdmin)
	}

	visible, hasMore, err := service.ListMessages(ctx, channelID, memberID, 10, nil)
	if err != nil {
		t.Fatalf("list messages after admin deletion: %v", err)
	}
	if hasMore || len(visible) != 2 {
		t.Fatalf("active messages after deleting one: got %d messages and hasMore=%v; want 2 and false", len(visible), hasMore)
	}
	for _, message := range visible {
		if message.ID == first.ID {
			t.Fatalf("soft-deleted message %d appeared in history", first.ID)
		}
	}

	if _, err := service.DeleteMessage(ctx, second.ID, authorID); err != nil {
		t.Fatalf("author deletes own message: %v", err)
	}
	if _, err := service.DeleteMessage(ctx, third.ID, ownerID); err != nil {
		t.Fatalf("owner deletes message: %v", err)
	}

	visible, hasMore, err = service.ListMessages(ctx, channelID, memberID, 10, nil)
	if err != nil {
		t.Fatalf("list messages after all deletions: %v", err)
	}
	if hasMore || len(visible) != 0 {
		t.Fatalf("active messages after all deletions: got %d messages and hasMore=%v; want 0 and false", len(visible), hasMore)
	}
}

func openMessageTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatalf("refusing to run integration test against database %q; database name must end in _test", config.ConnConfig.Database)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

func createMessageTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, displayName string) int64 {
	t.Helper()

	email := fmt.Sprintf("%s-%d@example.test", strings.ToLower(displayName), time.Now().UnixNano())
	var userID int64
	err := pool.QueryRow(ctx,
		"INSERT INTO users (display_name, email, password_hash) VALUES ($1, $2, $3) RETURNING id",
		displayName,
		email,
		"integration-test-hash",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("create test user %q: %v", displayName, err)
	}

	return userID
}
