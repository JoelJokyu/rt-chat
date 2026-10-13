package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"rtchat/internal/auth"
	"rtchat/internal/memberships"
	"rtchat/internal/messages"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChatHandlerAuthenticationBroadcastPersistenceAndDisconnect(t *testing.T) {
	pool := openRealtimeTestPool(t)
	ctx := context.Background()

	var serverID, channelID int64
	userIDs := make([]int64, 0, 3)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if serverID != 0 {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverID); err != nil {
				t.Errorf("clean up realtime test server: %v", err)
			}
		}
		for _, userID := range userIDs {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userID); err != nil {
				t.Errorf("clean up realtime test user %d: %v", userID, err)
			}
		}
	})

	ownerID := createRealtimeTestUser(t, ctx, pool, "SocketOwner")
	userIDs = append(userIDs, ownerID)
	memberID := createRealtimeTestUser(t, ctx, pool, "SocketMember")
	userIDs = append(userIDs, memberID)
	outsiderID := createRealtimeTestUser(t, ctx, pool, "SocketOutsider")
	userIDs = append(userIDs, outsiderID)

	serverName := fmt.Sprintf("realtime-test-%d", time.Now().UnixNano())
	if err := pool.QueryRow(ctx,
		"INSERT INTO servers (name) VALUES ($1) RETURNING id",
		serverName,
	).Scan(&serverID); err != nil {
		t.Fatalf("create realtime test server: %v", err)
	}
	for _, membership := range []struct {
		userID int64
		role   string
	}{{ownerID, "owner"}, {memberID, "member"}} {
		if _, err := pool.Exec(ctx,
			"INSERT INTO memberships (server_id, user_id, role) VALUES ($1, $2, $3)",
			serverID, membership.userID, membership.role,
		); err != nil {
			t.Fatalf("create %s membership: %v", membership.role, err)
		}
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO channels (server_id, name) VALUES ($1, $2) RETURNING id",
		serverID, "general",
	).Scan(&channelID); err != nil {
		t.Fatalf("create realtime test channel: %v", err)
	}

	sessionService := auth.NewSessionService(auth.NewSessionRepository(pool))
	createSession := func(userID int64) string {
		t.Helper()
		token, err := sessionService.CreateSession(ctx, userID, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("create session for user %d: %v", userID, err)
		}
		return token
	}
	ownerToken := createSession(ownerID)
	memberToken := createSession(memberID)
	outsiderToken := createSession(outsiderID)

	hub := NewHub()
	messageService := messages.NewMessageService(messages.NewMessageRepository(pool))
	membershipService := memberships.NewMembershipService(memberships.NewMembershipRepository(pool))
	handler := NewHandler(membershipService, messageService, hub)
	mux := http.NewServeMux()
	mux.Handle("GET /channels/{id}/ws", auth.AuthMiddleware(sessionService)(http.HandlerFunc(handler.ChatHandler)))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + fmt.Sprintf("/channels/%d/ws", channelID)

	dial := func(token string) (*websocket.Conn, *http.Response, error) {
		dialCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		headers := http.Header{}
		if token != "" {
			headers.Set("Cookie", (&http.Cookie{Name: "session_id", Value: token}).String())
		}
		return websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	}

	if conn, response, err := dial(""); err == nil {
		conn.CloseNow()
		t.Fatal("unauthenticated websocket connection succeeded; want rejection")
	} else if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated websocket status = %v; want %d", responseStatus(response), http.StatusUnauthorized)
	}

	if conn, response, err := dial(outsiderToken); err == nil {
		conn.CloseNow()
		t.Fatal("nonmember websocket connection succeeded; want rejection")
	} else if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("nonmember websocket status = %v; want %d", responseStatus(response), http.StatusForbidden)
	}

	ownerConn, response, err := dial(ownerToken)
	if err != nil {
		t.Fatalf("owner websocket dial failed (status %v): %v", responseStatus(response), err)
	}
	defer ownerConn.CloseNow()
	memberConn, response, err := dial(memberToken)
	if err != nil {
		t.Fatalf("member websocket dial failed (status %v): %v", responseStatus(response), err)
	}
	defer memberConn.CloseNow()
	waitForHubClientCount(t, hub, channelID, 2)

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	if err := ownerConn.Write(writeCtx, websocket.MessageText, []byte("persist and broadcast")); err != nil {
		cancelWrite()
		t.Fatalf("send websocket message: %v", err)
	}
	cancelWrite()

	ownerEvent := readMessageCreatedEvent(t, ownerConn, channelID)
	memberEvent := readMessageCreatedEvent(t, memberConn, channelID)
	if ownerEvent.Message.ID != memberEvent.Message.ID || ownerEvent.Message.Content != "persist and broadcast" {
		t.Fatalf("clients received inconsistent broadcast events: owner=%+v member=%+v", ownerEvent, memberEvent)
	}
	if ownerEvent.Message.AuthorID != ownerID {
		t.Fatalf("persisted message author = %d; want %d", ownerEvent.Message.AuthorID, ownerID)
	}

	page, _, err := messageService.ListMessages(ctx, channelID, memberID, 10, nil)
	if err != nil {
		t.Fatalf("read persisted websocket message: %v", err)
	}
	if len(page) != 1 || page[0].ID != ownerEvent.Message.ID {
		t.Fatalf("database history after websocket send = %+v; want the broadcast message", page)
	}

	if err := ownerConn.Close(websocket.StatusNormalClosure, "test disconnect"); err != nil {
		t.Fatalf("close owner websocket: %v", err)
	}
	waitForHubClientCount(t, hub, channelID, 1)
}

func openRealtimeTestPool(t *testing.T) *pgxpool.Pool {
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

func createRealtimeTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, displayName string) int64 {
	t.Helper()
	email := fmt.Sprintf("%s-%d@example.test", strings.ToLower(displayName), time.Now().UnixNano())
	var userID int64
	err := pool.QueryRow(ctx,
		"INSERT INTO users (display_name, email, password_hash) VALUES ($1, $2, $3) RETURNING id",
		displayName, email, "integration-test-hash",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("create realtime test user %q: %v", displayName, err)
	}
	return userID
}

func readMessageCreatedEvent(t *testing.T, conn *websocket.Conn, wantChannelID int64) messages.MessageEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read websocket event: %v", err)
	}
	var event messages.MessageEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("decode websocket event %q: %v", payload, err)
	}
	if event.Type != "message.created" || event.ChannelID != wantChannelID || event.Message == nil {
		t.Fatalf("websocket event = %+v; want message.created for channel %d", event, wantChannelID)
	}
	return event
}

func waitForHubClientCount(t *testing.T, hub *Hub, channelID int64, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		got := len(hub.clientsByChannel[channelID])
		hub.mu.RUnlock()
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	hub.mu.RLock()
	got := len(hub.clientsByChannel[channelID])
	hub.mu.RUnlock()
	t.Fatalf("hub has %d connected clients for channel %d; want %d", got, channelID, want)
}

func responseStatus(response *http.Response) string {
	if response == nil {
		return "no HTTP response"
	}
	return fmt.Sprintf("%d %s", response.StatusCode, http.StatusText(response.StatusCode))
}
