# RT Chat

**A real-time community messaging backend built with Go, PostgreSQL, and WebSockets.**

RT Chat provides the backend for server-based chat communities: users create
servers, organize conversations into channels, manage membership, and exchange
messages through both HTTP and persistent WebSocket connections.

## Features

- Account registration and cookie-based, PostgreSQL-backed sessions.
- Server creation, membership roles, join codes, and role-based authorization.
- Channel creation and member-scoped channel access.
- Paginated message history with keyset cursors.
- Message creation, editing, and soft deletion with author/moderator rules.
- Authenticated WebSocket chat with persisted messages and live channel events.
- PostgreSQL migrations, integration tests, and a repeatable performance test
  dataset and HTTP load generator.

## Technology

| Area | Implementation |
| --- | --- |
| Language | Go 1.25.6 |
| HTTP | `net/http` and `http.ServeMux` |
| Persistence | PostgreSQL with `pgx` / `pgxpool` |
| Realtime | WebSockets via `coder/websocket` |
| Password hashing | bcrypt |

## Design overview

The application is a modular monolith with a layered request path:

```text
HTTP router and middleware -> handlers -> services -> repositories -> PostgreSQL
```

The domain is centered on users, servers, memberships, channels, and messages.
Membership rows hold the `member`, `admin`, or `owner` role; ownership is not
duplicated in a separate server column. PostgreSQL is the durable store for
messages and sessions. An in-process WebSocket hub groups live connections by
channel and distributes message events within the running API instance.

The current implementation is single-instance: WebSocket fan-out between
separate API processes is not provided.

## Run locally

### Requirements

- Go 1.25.6
- PostgreSQL
- A development database and a PostgreSQL connection URL

The Go module and commands are in `server/`. The application reads
`DATABASE_URL` from the process environment; it does not load `.env` files.

PowerShell example (use your local database credentials):

```powershell
$env:DATABASE_URL = "postgres://rtchat_app:YOUR_PASSWORD@localhost:5432/rtchat_dev?sslmode=disable"
Set-Location server
go run ./cmd/migrate
go run ./cmd/api
```

The API listens on `http://localhost:8080`. Verify startup with:

```powershell
curl.exe http://localhost:8080/health
```

The migration runner applies numbered SQL files once and records them in
`schema_migrations`. Those migration files define the current database schema.

## API surface

All routes except registration, login, and health require the `session_id`
cookie issued at login.

| Resource | Implemented routes |
| --- | --- |
| Health and identity | `GET /health`, `GET /me` |
| Accounts and sessions | `POST /users`, `POST /login`, `POST /logout` |
| Servers | `POST /servers`, `GET /servers`, `GET /servers/{id}`, `PATCH /servers/{id}`, `DELETE /servers/{id}` |
| Channels | `POST /servers/{id}/channels`, `GET /servers/{id}/channels` |
| Messages | `POST /channels/{id}/messages`, `GET /channels/{id}/messages`, `PATCH /messages/{id}`, `DELETE /messages/{id}` |
| Join codes | `POST /servers/{id}/join-codes`, `POST /join`, `DELETE /servers/{id}/join-codes/{codeID}` |
| Membership | `GET /servers/{id}/members`, `PATCH /servers/{id}/members/{userID}`, `DELETE /servers/{id}/members/{userID}` |
| Realtime | `GET /channels/{id}/ws` (WebSocket upgrade) |

## Authentication and authorization

Login issues a random session token in an HTTP-only `session_id` cookie. The
database stores a SHA-256 hash of the token, not the raw value. Sessions expire
after 72 hours and logout revokes the current session.

Server permissions are derived from membership roles. Owners manage servers,
join codes, and member roles; admins can create channels and moderate messages
and regular members; members can access server content and manage their own
messages. Server and message authorization is enforced in the application
service/repository path as well as by authenticated routing.

**Deployment note:** the cookie currently has `Secure` disabled for local
development. A production deployment must use HTTPS and set secure cookie
attributes appropriately.

## Realtime messaging

Connect to `GET /channels/{id}/ws` with the authenticated session cookie. The
server checks channel membership before upgrading the connection. A client
sends a text frame containing message content; the server persists the message
and broadcasts a JSON event to connections in that channel. HTTP message
create, edit, and delete operations broadcast the same event types:
`message.created`, `message.updated`, and `message.deleted`.

## Testing

PostgreSQL integration tests use a separate database URL named with the
`_test` suffix. From the repository root in PowerShell:

```powershell
$env:TEST_DATABASE_URL = "postgres://rtchat_app:YOUR_PASSWORD@localhost:5432/rtchat_test?sslmode=disable"
$env:DATABASE_URL = $env:TEST_DATABASE_URL
Set-Location server
go run ./cmd/migrate
go test ./...
```

The integration tests skip when `TEST_DATABASE_URL` is unset. The migration
command reads `DATABASE_URL`; the assignment above ensures it targets the test
database. Keep the test and development URLs separate. Tests create and remove
temporary records in the test database.

## Performance testing

`cmd/seedperf` creates a synthetic dataset and `cmd/loadtest` exercises the
message HTTP API against a running server. Both tools require
`TEST_DATABASE_URL` to refer to a database ending in `_test` and refuse to use
`DATABASE_URL`. The seeder replaces only its marked dataset; the load test
cleans up its own messages and temporary session.

Run these tools only against a disposable test database. Test setup, measured
results, and limitations are documented in
[database performance notes](docs/database-performance-decisions.md).
