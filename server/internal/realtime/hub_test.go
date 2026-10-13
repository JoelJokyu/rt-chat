package realtime

import (
	"bytes"
	"testing"
)

func TestHubBroadcastsOnlyToRegisteredChannelClients(t *testing.T) {
	hub := NewHub()

	firstClient := NewClient(nil, 1)
	secondClient := NewClient(nil, 2)
	otherChannelClient := NewClient(nil, 3)

	const firstChannelID int64 = 10
	const secondChannelID int64 = 20
	hub.Register(firstChannelID, firstClient)
	hub.Register(firstChannelID, secondClient)
	hub.Register(secondChannelID, otherChannelClient)

	firstPayload := []byte(`{"type":"message.created","message_id":101}`)
	hub.Broadcast(firstChannelID, firstPayload)

	assertQueuedPayload(t, firstClient, firstPayload)
	assertQueuedPayload(t, secondClient, firstPayload)
	assertNoQueuedPayload(t, otherChannelClient)

	hub.Unregister(firstChannelID, secondClient)

	secondPayload := []byte(`{"type":"message.updated","message_id":101}`)
	hub.Broadcast(firstChannelID, secondPayload)

	assertQueuedPayload(t, firstClient, secondPayload)
	assertNoQueuedPayload(t, secondClient)
	assertNoQueuedPayload(t, otherChannelClient)
}

func assertQueuedPayload(t *testing.T, client *Client, want []byte) {
	t.Helper()

	select {
	case got := <-client.send:
		if !bytes.Equal(got, want) {
			t.Fatalf("queued payload = %q; want %q", got, want)
		}
	default:
		t.Fatal("expected a queued payload, got none")
	}
}

func assertNoQueuedPayload(t *testing.T, client *Client) {
	t.Helper()

	select {
	case got := <-client.send:
		t.Fatalf("unexpected queued payload %q", got)
	default:
	}
}
