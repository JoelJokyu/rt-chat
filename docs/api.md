# RT Chat API

This reference describes routes currently registered by the API. It does not
include planned endpoints. The server listens on port `8080` by default, so
examples use `http://localhost:8080`.

## Conventions

- JSON request bodies use `Content-Type: application/json`.
- Successful JSON responses use `Content-Type: application/json`.
- Error responses are plain text, produced by Go's `http.Error`.
- IDs are positive integer path parameters.
- Timestamps are RFC 3339 values. Cursor timestamps accept RFC 3339 with
  fractional seconds.
- Registration, login, and health are public. All other HTTP routes require a
  valid `session_id` cookie.

### Session cookie

`POST /login` sets an HTTP-only cookie named `session_id`. The API stores only
the SHA-256 hash of the token in PostgreSQL. Sessions expire after 72 hours;
`POST /logout` revokes the current session and expires the cookie. The cookie
currently has `Secure` disabled for local development, so production use must
be over HTTPS with secure cookie settings.

For command-line use, preserve the cookie between requests:

```sh
curl -i -c cookies.txt -X POST http://localhost:8080/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}'

curl -b cookies.txt http://localhost:8080/me
```

## Route summary

| Method | Path | Purpose | Access |
| --- | --- | --- | --- |
| `GET` | `/health` | Liveness response | Public |
| `POST` | `/users` | Register an account | Public |
| `POST` | `/login` | Create a session | Public |
| `GET` | `/me` | Get the authenticated user ID | Authenticated |
| `POST` | `/logout` | Revoke the current session | Authenticated |
| `POST` | `/servers` | Create a server | Authenticated |
| `GET` | `/servers` | List the user's servers | Authenticated |
| `GET` | `/servers/{id}` | Get a server the user belongs to | Authenticated member |
| `PATCH` | `/servers/{id}` | Rename a server | Owner |
| `DELETE` | `/servers/{id}` | Delete a server | Owner |
| `POST` | `/servers/{id}/channels` | Create a channel | Owner or admin |
| `GET` | `/servers/{id}/channels` | List server channels | Member |
| `POST` | `/channels/{id}/messages` | Create a message | Member |
| `GET` | `/channels/{id}/messages` | List message history | Member |
| `PATCH` | `/messages/{id}` | Edit a message | Message author |
| `DELETE` | `/messages/{id}` | Soft-delete a message | Author, admin, or owner |
| `POST` | `/servers/{id}/join-codes` | Create a join code | Owner |
| `POST` | `/join` | Join a server with a code | Authenticated |
| `DELETE` | `/servers/{id}/join-codes/{codeID}` | Revoke a join code | Owner |
| `GET` | `/servers/{id}/members` | List server members | Member |
| `PATCH` | `/servers/{id}/members/{userID}` | Set a member's role | Owner |
| `DELETE` | `/servers/{id}/members/{userID}` | Remove a regular member | Owner or admin |
| `GET` | `/channels/{id}/ws` | Open a channel WebSocket | Member |

## Health and identity

### `GET /health`

Returns `200 OK` when the HTTP process responds.

```json
{"message":"healthy"}
```

### `GET /me`

Returns `200 OK` with the authenticated user's ID.

```json
{"user_id":1}
```

## Accounts and sessions

### `POST /users`

Creates an account. Display names are trimmed and limited to 16 characters;
email is trimmed and lowercased; passwords must contain at least 8 bytes.

Request:

```json
{
  "display_name":"Alice",
  "email":"alice@example.com",
  "password":"password123"
}
```

Returns `201 Created` with the public user representation. The password and
password hash are not returned.

```json
{
  "id":1,
  "display_name":"Alice",
  "email":"alice@example.com",
  "created_at":"2026-10-13T10:00:00Z",
  "updated_at":"2026-10-13T10:00:00Z"
}
```

Common errors: `400 Bad Request` for invalid input or malformed JSON;
`409 Conflict` if the email is already registered.

### `POST /login`

Request:

```json
{"email":"alice@example.com","password":"password123"}
```

Returns `200 OK` with the same public user representation as registration and
sets the `session_id` cookie.

Common errors: `400 Bad Request` for missing/invalid input; `401 Unauthorized`
for invalid credentials.

### `POST /logout`

Revokes the current session, expires the `session_id` cookie, and returns
`204 No Content`.

## Servers

The server representation is:

```json
{
  "id":12,
  "name":"Engineering",
  "created_at":"2026-10-13T10:00:00Z",
  "updated_at":"2026-10-13T10:00:00Z"
}
```

### `POST /servers`

Creates a server and makes the authenticated user its owner in the same
database transaction. Request: `{"name":"Engineering"}`. The name must not
be blank. Returns `201 Created` with a server representation.

### `GET /servers`

Returns `200 OK` with an array of servers the authenticated user belongs to.
The array is empty when the user has no servers.

### `GET /servers/{id}`

Returns `200 OK` with the server representation. A missing server and a server
the caller does not belong to both currently return `404 Not Found`.

### `PATCH /servers/{id}`

Owner-only rename. Request: `{"name":"New name"}`. Returns `200 OK` with the
updated server. The name must not be blank. A caller who is not the owner
receives `403 Forbidden`.

### `DELETE /servers/{id}`

Owner-only. Deletes the server and its dependent memberships, channels, join
codes, and messages through database cascades. Returns `204 No Content`. A
caller who is not the owner receives `403 Forbidden`.

## Channels

Channel representation:

```json
{
  "id":31,
  "server_id":12,
  "name":"general",
  "created_at":"2026-10-13T10:00:00Z",
  "updated_at":"2026-10-13T10:00:00Z"
}
```

### `POST /servers/{id}/channels`

Owner/admin-only. Request: `{"name":"general"}`. The name must not be blank
and need not be unique within the server. Returns `201 Created` with the
channel. Non-admin members receive `403 Forbidden`.

### `GET /servers/{id}/channels`

Returns `200 OK` with an array of channels. A non-member receives
`403 Forbidden`; a server with no channels returns an empty array.

## Messages

Message representation:

```json
{
  "id":101,
  "channel_id":31,
  "content":"Hello!",
  "created_at":"2026-10-13T10:00:00Z",
  "author_id":1,
  "updated_at":"2026-10-13T10:05:00Z"
}
```

`updated_at` is omitted until the message is edited. Deleted messages are
soft-deleted and omitted from history.

### `POST /channels/{id}/messages`

Creates a message in a channel accessible to the authenticated member.
Request: `{"content":"Hello!"}`. Content must not be blank. Returns
`201 Created` with the message and broadcasts a `message.created` event to
connected clients in the channel.

### `GET /channels/{id}/messages`

Returns message history newest-first using keyset pagination.

Query parameters:

| Parameter | Meaning |
| --- | --- |
| `limit` | Page size; defaults to `50`, values above `100` are capped at `100`; must be positive |
| `before_created_at` | Timestamp cursor from the last message of the previous page |
| `before_id` | ID cursor from the same message; required together with `before_created_at` |

Example:

```text
GET /channels/31/messages?limit=50&before_created_at=2026-10-13T10%3A00%3A00Z&before_id=101
```

Response:

```json
{
  "messages":[
    {"id":101,"channel_id":31,"content":"Hello!","created_at":"2026-10-13T10:00:00Z","author_id":1}
  ],
  "has_more":true,
  "next_cursor":{"created_at":"2026-10-13T10:00:00Z","id":101}
}
```

`next_cursor` is omitted when there is no next page. For the next request, pass
its `created_at` as `before_created_at` and `id` as `before_id`. A non-member
receives `403 Forbidden`; malformed limits or cursors receive `400 Bad
Request`.

### `PATCH /messages/{id}`

Only the message author can edit. Request: `{"content":"Corrected text"}`.
Content must not be blank. Returns `200 OK` with the updated message and
broadcasts `message.updated`. Other users receive `403 Forbidden`.

### `DELETE /messages/{id}`

The author, a server admin, or the server owner can soft-delete the message.
Returns `204 No Content` and broadcasts `message.deleted`. Other users receive
`403 Forbidden`.

## Join codes

### `POST /servers/{id}/join-codes`

Owner-only. The expiry must be in the future.

Request:

```json
{"expires_at":"2026-10-14T10:00:00Z"}
```

Returns `201 Created`:

```json
{
  "join_code":{
    "id":7,
    "server_id":12,
    "created_by":1,
    "created_at":"2026-10-13T10:00:00Z",
    "expires_at":"2026-10-14T10:00:00Z"
  },
  "code":"one-time-raw-code"
}
```

The raw code is returned at creation; only its hash is stored. Share the raw
code with the intended user. Non-owners receive `403 Forbidden`.

### `POST /join`

Joins the authenticated user to the server identified by a valid, unexpired,
unrevoked code. Request: `{"code":"one-time-raw-code"}`.

New membership returns `201 Created`; an existing membership returns
`200 OK`:

```json
{"server_id":12,"already_member":false}
```

Missing, invalid, expired, or revoked codes currently return `400 Bad Request`.

### `DELETE /servers/{id}/join-codes/{codeID}`

Owner-only revocation. Returns `204 No Content`. Non-owners receive
`403 Forbidden`.

## Membership

Member representation:

```json
{
  "user_id":1,
  "display_name":"Alice",
  "role":"owner",
  "joined_at":"2026-10-13T10:00:00Z"
}
```

### `GET /servers/{id}/members`

Member-only, newest memberships first, with keyset pagination.

| Parameter | Meaning |
| --- | --- |
| `limit` | Page size; defaults to `10`, values above `20` are capped at `20`; must be positive |
| `before_joined_at` | Timestamp cursor from the last member of the previous page |
| `before_user_id` | User ID cursor from that same member; required together with `before_joined_at` |

Response has the same pagination envelope as messages, with `members` in
place of `messages`; `next_cursor` contains `joined_at` and `user_id`. A
non-member receives `403 Forbidden`.

### `PATCH /servers/{id}/members/{userID}`

Owner-only. Sets the target's role to `member` or `admin`:

```json
{"role":"admin"}
```

Returns `200 OK` with `{"user_id":2,"role":"admin"}`. The owner role cannot
be assigned or changed through this endpoint; the owner cannot be modified.
Only an owner can change roles.

### `DELETE /servers/{id}/members/{userID}`

An owner or admin can remove a regular member. Owners and admins cannot be
removed through this route; an owner must first demote an admin to `member`.
Returns `200 OK` with `{"updated":true}`. Forbidden targets/actors receive
`403 Forbidden`.

## WebSocket

### `GET /channels/{id}/ws`

Open a WebSocket connection with the `session_id` cookie. The server checks
membership before upgrading: missing/invalid session returns `401 Unauthorized`
and a non-member returns `403 Forbidden`.

The client sends a text frame whose contents are the message text itself, not
a JSON object. Each accepted message is stored in PostgreSQL and published
through Redis Pub/Sub; connected clients receive a JSON event through their
API instance's local hub. HTTP create, edit, and delete operations publish the
corresponding event types as well. Delivery is transient: Redis Pub/Sub does
not replay events missed while disconnected, and a publish failure after the
database write is logged without undoing the write. There is no separate client
acknowledgment or structured error event in the current protocol.

Event envelope for creation and update:

```json
{
  "type":"message.created",
  "channel_id":31,
  "message":{
    "id":101,
    "channel_id":31,
    "content":"Hello!",
    "created_at":"2026-10-13T10:00:00Z",
    "author_id":1
  }
}
```

`message.updated` uses the same envelope with the updated message. Deletion
uses a message ID instead:

```json
{"type":"message.deleted","channel_id":31,"message_id":101}
```

The hub is process-local. A connection is authorized when it opens; the
current implementation does not re-check membership if that membership is
later revoked. Broadcasts reach only clients connected to the same API
process.

## Error and status behavior

Common status codes:

| Status | Meaning |
| --- | --- |
| `200 OK` | Successful read/update, existing membership on join, or member removal response |
| `201 Created` | Account, server, channel, message, join code, or new membership created |
| `204 No Content` | Successful logout, server/message deletion, or join-code revocation |
| `400 Bad Request` | Malformed JSON, invalid path/query/cursor/input, or invalid join code |
| `401 Unauthorized` | Missing, invalid, or expired session; invalid login credentials |
| `403 Forbidden` | Authenticated user lacks the required membership or role |
| `404 Not Found` | Server lookup returned no server visible to the caller |
| `409 Conflict` | Registration email already exists |
| `500 Internal Server Error` | Unexpected server or database failure |

The implementation currently uses plain-text error bodies; error messages may
vary by route.
