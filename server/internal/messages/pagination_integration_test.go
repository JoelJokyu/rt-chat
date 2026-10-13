package messages

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestListMessagesPaginationWithMatchingTimestamps(t *testing.T) {
	pool := openMessageTestPool(t)
	ctx := context.Background()

	userID := createMessageTestUser(t, ctx, pool, "PageAuthor")
	var serverID int64
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if serverID != 0 {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverID); err != nil {
				t.Errorf("clean up test server: %v", err)
			}
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userID); err != nil {
			t.Errorf("clean up test user: %v", err)
		}
	})

	serverName := fmt.Sprintf("message-page-test-%d", time.Now().UnixNano())
	if err := pool.QueryRow(ctx,
		"INSERT INTO servers (name) VALUES ($1) RETURNING id",
		serverName,
	).Scan(&serverID); err != nil {
		t.Fatalf("create test server: %v", err)
	}

	if _, err := pool.Exec(ctx,
		"INSERT INTO memberships (server_id, user_id, role) VALUES ($1, $2, 'owner')",
		serverID,
		userID,
	); err != nil {
		t.Fatalf("create owner membership: %v", err)
	}

	var channelID int64
	if err := pool.QueryRow(ctx,
		"INSERT INTO channels (server_id, name) VALUES ($1, $2) RETURNING id",
		serverID,
		"general",
	).Scan(&channelID); err != nil {
		t.Fatalf("create test channel: %v", err)
	}

	// Identical timestamps force pagination to use the ID tie-breaker.
	createdAt := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	expectedIDs := make([]int64, 0, 5)
	for i := 1; i <= 5; i++ {
		var messageID int64
		content := fmt.Sprintf("pagination message %d", i)
		err := pool.QueryRow(ctx,
			"INSERT INTO messages (channel_id, author_id, content, created_at) VALUES ($1, $2, $3, $4) RETURNING id",
			channelID,
			userID,
			content,
			createdAt,
		).Scan(&messageID)
		if err != nil {
			t.Fatalf("insert pagination message %d: %v", i, err)
		}
		expectedIDs = append(expectedIDs, messageID)
	}

	service := NewMessageService(NewMessageRepository(pool))
	var allFetchedIDs []int64
	var cursor *MessageCursor

	for pageNumber := 1; pageNumber <= 3; pageNumber++ {
		page, hasMore, err := service.ListMessages(ctx, channelID, userID, 2, cursor)
		if err != nil {
			t.Fatalf("fetch page %d: %v", pageNumber, err)
		}

		wantHasMore := pageNumber < 3
		if hasMore != wantHasMore {
			t.Fatalf("page %d hasMore = %v; want %v", pageNumber, hasMore, wantHasMore)
		}

		wantLength := 2
		if pageNumber == 3 {
			wantLength = 1
		}
		if len(page) != wantLength {
			t.Fatalf("page %d contains %d messages; want %d", pageNumber, len(page), wantLength)
		}

		for _, message := range page {
			allFetchedIDs = append(allFetchedIDs, message.ID)
		}

		if hasMore {
			last := page[len(page)-1]
			cursor = &MessageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}
	}

	for i, expectedIndex := range []int{4, 3, 2, 1, 0} {
		if allFetchedIDs[i] != expectedIDs[expectedIndex] {
			t.Fatalf("fetched ID at position %d = %d; want %d; all IDs: %v", i, allFetchedIDs[i], expectedIDs[expectedIndex], allFetchedIDs)
		}
	}
}
