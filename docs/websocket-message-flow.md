# RT Chat WebSocket Message Flow

This document describes the current implementation in
`server/internal/realtime/` and message events initiated by
`server/internal/messages/`. Each API process has an in-memory hub for its own
WebSocket connections; Redis Pub/Sub carries events between those process-local
hubs.

## 1. Connection and authorization

```mermaid
sequenceDiagram
    autonumber
    participant C as Client
    participant R as HTTP Router / Auth Middleware
    participant H as WebSocket Handler
    participant DB as PostgreSQL
    participant Hub as This Process's Hub
    participant W as Client Write Loop

    C->>R: GET /channels/{id}/ws + session_id cookie
    R->>DB: Find active, unexpired session by token hash
    DB-->>R: Session / no match
    alt Missing, invalid, revoked, or expired session
        R-->>C: HTTP 401 Unauthorized
    else Valid session
        R->>H: Request with session in context
        H->>DB: Check user membership in channel's server
        DB-->>H: Member? yes/no
        alt Not a member
            H-->>C: HTTP 403 Forbidden
        else Is a member
            H->>C: Upgrade HTTP connection to WebSocket
            H->>Hub: Register client for channel
            H->>W: Start write loop
            Note over H: Handler runs the read loop
        end
    end
```

Authentication occurs before the WebSocket upgrade. Channel membership is
checked once, when the connection is opened. The current implementation does
not re-check membership if the user is removed while the connection remains
open.

## 2. Message sent over WebSocket

The client sends a WebSocket **text frame** containing the message content
itself, for example `Hello there`. The inbound frame is not a JSON request
object and there is no separate client message ID or command type.

```mermaid
sequenceDiagram
    autonumber
    participant C as Sending Client
    participant Read as Client Read Loop
    participant S as Message Service
    participant DB as PostgreSQL
    participant Redis as Redis Pub/Sub
    participant SubA as API A Subscriber
    participant HubA as API A Local Hub
    participant SubB as API B Subscriber
    participant HubB as API B Local Hub
    participant Q as Connected Client Queue(s)
    participant Write as Each Client Write Loop
    participant Recipients as Clients on Both Instances

    C->>Read: Text frame with message content
    Read->>S: Create(channel ID, authenticated user ID, content)
    S->>DB: INSERT message, requiring channel membership
    DB-->>S: Persisted message
    S-->>Read: Message with database ID and timestamp
    Read->>Read: Encode message.created event as JSON
    Read->>Redis: PUBLISH event JSON
    par Subscriber on API A (originating process)
        Redis-->>SubA: Deliver event JSON
        SubA->>HubA: Broadcast(channel ID, event bytes)
        HubA->>Q: Enqueue for API A clients in channel
    and Subscriber on API B (another process)
        Redis-->>SubB: Deliver event JSON
        SubB->>HubB: Broadcast(channel ID, event bytes)
        HubB->>Q: Enqueue for API B clients in channel
    end
    Q->>Write: Event available on send queue
    Write->>Recipients: WebSocket text frame with JSON event
```

### Ordering and event shape

The message is inserted before the event is published. Thus the event contains
the persisted message ID and timestamp, and a later history request can read
the message. The sending client receives the event through its API process's
Redis subscriber like other local clients; the publisher does not separately
broadcast to the local hub. There is no distinct acknowledgement protocol.

Creation event:

```json
{
  "type": "message.created",
  "channel_id": 31,
  "message": {
    "id": 101,
    "channel_id": 31,
    "content": "Hello there",
    "created_at": "2026-10-13T10:00:00Z",
    "author_id": 1
  }
}
```

The server currently has no structured WebSocket error event. If message
creation fails (for example, blank content), the failure is logged and the
read loop continues; no message event is published and no error response is
sent to the client. If creation succeeds but Redis publication fails, the
message remains in PostgreSQL, the failure is logged, and connected clients do
not receive that live event.

## 3. Message changed over HTTP

HTTP message create, edit, and delete handlers publish to Redis after the
service/repository operation succeeds. Each subscriber then broadcasts to its
process-local channel hub:

```mermaid
sequenceDiagram
    autonumber
    participant C as HTTP Client
    participant H as Message Handler
    participant S as Message Service
    participant DB as PostgreSQL
    participant Redis as Redis Pub/Sub
    participant SubA as API A Subscriber
    participant HubA as API A Local Hub
    participant SubB as API B Subscriber
    participant HubB as API B Local Hub
    participant WS as Connected WebSocket Clients

    C->>H: POST /channels/{id}/messages, PATCH /messages/{id}, or DELETE /messages/{id}
    H->>S: Authenticate user and perform authorized operation
    S->>DB: Insert, update, or soft-delete message
    DB-->>S: Operation result
    S-->>H: Saved message / deleted message ID
    H->>Redis: Publish corresponding JSON event
    par API A subscriber
        Redis-->>SubA: Deliver event
        SubA->>HubA: Broadcast to local channel clients
        HubA->>WS: Enqueue event
    and API B subscriber
        Redis-->>SubB: Deliver event
        SubB->>HubB: Broadcast to local channel clients
        HubB->>WS: Enqueue event
    end
    H-->>C: HTTP response (201, 200, or 204)
```

Events are:

- `message.created`: contains the full persisted message.
- `message.updated`: contains the updated message, including `updated_at`.
- `message.deleted`: contains `channel_id` and `message_id`; deletion is a
  soft delete in PostgreSQL.

The event is published before the handler writes the successful HTTP response.
The HTTP client may itself have a WebSocket connection and therefore receive
the event through its API process's Redis subscriber. If Redis publication
fails after the database operation succeeds, the handler logs the failure and
still returns the successful HTTP response; the persisted change remains
available through message history.

## 4. Disconnects and slow clients

Each client has a buffered outbound queue with capacity 16. The hub copies the
registered-client set while holding its read lock, then queues the event for
each client without holding that lock. This keeps network writes out of the
hub's critical section.

If a client's queue is full, enqueueing fails. The hub unregisters and closes
that client rather than allowing one slow connection to stall broadcasts to
the rest of the channel. A read error or write error also ends the connection;
cleanup unregisters the client and closes its socket. The client has one read
loop and one write loop, so concurrent hub broadcasts are serialized through
the outbound queue for each connection.

## Current boundaries

- Each hub exists only in its API process's memory. Redis Pub/Sub distributes
  events so connected clients on other API instances can receive them.
- Redis Pub/Sub is transient: it does not persist or replay events, and there
  is no delivery guarantee for disconnected subscribers. PostgreSQL remains
  the durable message source of truth.
- If publishing fails after a message write, the failure is logged but the
  database write is not rolled back. Connected clients can refresh history to
  observe the saved change.
- Membership is checked at connection time, not continuously during the
  connection.
- WebSocket writes are not persisted as a separate event stream; PostgreSQL
  messages are the durable record.
- There is no structured error/ack event, replay after reconnect, or delivery
  guarantee to a disconnected client. Clients can retrieve current history
  through `GET /channels/{id}/messages` after reconnecting.
- The current client protocol sends message text directly in a text frame;
  server-to-client events use JSON envelopes.

These are descriptions of current behavior, not a list of commitments about
future protocol design.
