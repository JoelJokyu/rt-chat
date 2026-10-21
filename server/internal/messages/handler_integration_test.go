package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rtchat/internal/auth"
)

func TestMessageHTTPHandlersAndBroadcasts(t *testing.T) {
	pool := openMessageTestPool(t)
	ctx := context.Background()

	var serverID int64
	userIDs := make([]int64, 0, 4)
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

	ownerID := createMessageTestUser(t, ctx, pool, "HTTPOwner")
	userIDs = append(userIDs, ownerID)
	adminID := createMessageTestUser(t, ctx, pool, "HTTPAdmin")
	userIDs = append(userIDs, adminID)
	authorID := createMessageTestUser(t, ctx, pool, "HTTPAuthor")
	userIDs = append(userIDs, authorID)
	memberID := createMessageTestUser(t, ctx, pool, "HTTPMember")
	userIDs = append(userIDs, memberID)

	serverName := fmt.Sprintf("message-http-test-%d", time.Now().UnixNano())
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

	sessionService := auth.NewSessionService(auth.NewSessionRepository(pool))
	createSessionToken := func(userID int64) string {
		t.Helper()
		token, err := sessionService.CreateSession(ctx, userID, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("create session for user %d: %v", userID, err)
		}
		return token
	}

	authorToken := createSessionToken(authorID)
	adminToken := createSessionToken(adminID)
	memberToken := createSessionToken(memberID)

	broadcaster := &recordingMessagePublisher{}
	service := NewMessageService(NewMessageRepository(pool))
	handler := NewMessageHandler(service, broadcaster)

	mux := http.NewServeMux()
	requireAuth := auth.AuthMiddleware(sessionService)
	mux.Handle("POST /channels/{id}/messages", requireAuth(http.HandlerFunc(handler.CreateMessageHandler)))
	mux.Handle("GET /channels/{id}/messages", requireAuth(http.HandlerFunc(handler.ListMessagesHandler)))
	mux.Handle("PATCH /messages/{id}", requireAuth(http.HandlerFunc(handler.UpdateMessageHandler)))
	mux.Handle("DELETE /messages/{id}", requireAuth(http.HandlerFunc(handler.DeleteMessageHandler)))

	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "session_id", Value: token})
		}
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, req)
		return recorder
	}

	unauthenticated := request(http.MethodPost,
		fmt.Sprintf("/channels/%d/messages", channelID),
		`{"content":"should not be created"}`,
		"",
	)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d; want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if len(broadcaster.events) != 0 {
		t.Fatalf("unauthenticated request broadcast %d events; want 0", len(broadcaster.events))
	}

	createdResponse := request(http.MethodPost,
		fmt.Sprintf("/channels/%d/messages", channelID),
		`{"content":"hello from the HTTP handler"}`,
		authorToken,
	)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d; want %d; body: %s", createdResponse.Code, http.StatusCreated, createdResponse.Body.String())
	}

	var created Message
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID <= 0 || created.ChannelID != channelID || created.AuthorID != authorID {
		t.Fatalf("create response has unexpected fields: %+v", created)
	}
	assertRecordedMessageEvent(t, broadcaster, 0, "message.created", channelID, created.ID)

	updateResponse := request(http.MethodPatch,
		fmt.Sprintf("/messages/%d", created.ID),
		`{"content":"edited through HTTP"}`,
		authorToken,
	)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d; want %d; body: %s", updateResponse.Code, http.StatusOK, updateResponse.Body.String())
	}
	var updated Message
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Content != "edited through HTTP" || updated.UpdatedAt == nil {
		t.Fatalf("update response has unexpected fields: %+v", updated)
	}
	assertRecordedMessageEvent(t, broadcaster, 1, "message.updated", channelID, created.ID)

	forbiddenUpdate := request(http.MethodPatch,
		fmt.Sprintf("/messages/%d", created.ID),
		`{"content":"admin cannot edit another author's message"}`,
		adminToken,
	)
	if forbiddenUpdate.Code != http.StatusForbidden {
		t.Fatalf("admin edit status = %d; want %d; body: %s", forbiddenUpdate.Code, http.StatusForbidden, forbiddenUpdate.Body.String())
	}
	if len(broadcaster.events) != 2 {
		t.Fatalf("forbidden edit changed event count to %d; want 2", len(broadcaster.events))
	}

	forbiddenDelete := request(http.MethodDelete,
		fmt.Sprintf("/messages/%d", created.ID),
		"",
		memberToken,
	)
	if forbiddenDelete.Code != http.StatusForbidden {
		t.Fatalf("regular member delete status = %d; want %d; body: %s", forbiddenDelete.Code, http.StatusForbidden, forbiddenDelete.Body.String())
	}
	if len(broadcaster.events) != 2 {
		t.Fatalf("forbidden delete changed event count to %d; want 2", len(broadcaster.events))
	}

	deleteResponse := request(http.MethodDelete,
		fmt.Sprintf("/messages/%d", created.ID),
		"",
		adminToken,
	)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("admin delete status = %d; want %d; body: %s", deleteResponse.Code, http.StatusNoContent, deleteResponse.Body.String())
	}
	assertRecordedMessageEvent(t, broadcaster, 2, "message.deleted", channelID, created.ID)

	listResponse := request(http.MethodGet,
		fmt.Sprintf("/channels/%d/messages", channelID),
		"",
		memberToken,
	)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d; want %d; body: %s", listResponse.Code, http.StatusOK, listResponse.Body.String())
	}
	var page struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(page.Messages) != 0 {
		t.Fatalf("history returned %d messages after deletion; want 0", len(page.Messages))
	}
}

type recordedMessageEvent struct {
	channelID int64
	payload   []byte
}

type recordingMessagePublisher struct {
	events []recordedMessageEvent
}

func (b *recordingMessagePublisher) Publish(_ context.Context, payload []byte) error {
	var event MessageEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	b.events = append(b.events, recordedMessageEvent{
		channelID: event.ChannelID,
		payload:   append([]byte(nil), payload...),
	})
	return nil
}

func assertRecordedMessageEvent(t *testing.T, publisher *recordingMessagePublisher, index int, wantType string, wantChannelID, wantMessageID int64) {
	t.Helper()

	if index >= len(publisher.events) {
		t.Fatalf("event index %d missing; only %d events recorded", index, len(publisher.events))
	}

	recorded := publisher.events[index]
	if recorded.channelID != wantChannelID {
		t.Fatalf("broadcast channel ID = %d; want %d", recorded.channelID, wantChannelID)
	}

	var event MessageEvent
	if err := json.Unmarshal(recorded.payload, &event); err != nil {
		t.Fatalf("decode broadcast event: %v", err)
	}
	if event.Type != wantType || event.ChannelID != wantChannelID {
		t.Fatalf("broadcast event = %+v; want type %q and channel %d", event, wantType, wantChannelID)
	}

	var messageID int64
	if wantType == "message.deleted" {
		messageID = event.MessageID
	} else {
		if event.Message == nil {
			t.Fatalf("broadcast event %q has no message payload", wantType)
		}
		messageID = event.Message.ID
	}
	if messageID != wantMessageID {
		t.Fatalf("broadcast message ID = %d; want %d", messageID, wantMessageID)
	}
}
