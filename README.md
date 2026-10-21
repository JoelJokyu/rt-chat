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
- Authenticated WebSocket chat with persisted messages and live channel events
  fanned out across API instances through Redis Pub/Sub.
- PostgreSQL migrations, integration tests, and a repeatable performance test
  dataset and HTTP load generator.

## Technology

| Area | Implementation |
| --- | --- |
| Language | Go 1.26+ |
| HTTP | `net/http` and `http.ServeMux` |
| Persistence | PostgreSQL with `pgx` / `pgxpool` |
| Realtime | WebSockets via `coder/websocket`; Redis Pub/Sub for cross-instance fan-out |
| Password hashing | bcrypt |

## Design overview

The application is a modular monolith with a layered request path:

```text
HTTP router and middleware -> handlers -> services -> repositories -> PostgreSQL
```

The domain is centered on users, servers, memberships, channels, and messages.
Membership rows hold the `member`, `admin`, or `owner` role; ownership is not
duplicated in a separate server column. PostgreSQL is the durable store for
messages and sessions. Each API process has an in-memory WebSocket hub for its
own connections; Redis Pub/Sub carries message events between processes so
each hub can deliver them to its local clients. Redis is a transient fan-out
mechanism, not the message store or a replay queue.

## Run with Docker Compose

Docker Compose starts PostgreSQL, Redis, and two API instances. Create a root
`.env` file containing a `POSTGRES_PASSWORD` value, then run:

```powershell
docker compose up -d db redis
docker compose build api
docker compose run --rm api ./migrate
docker compose up -d api api2
```

The API is available at `http://localhost:8080` and
`http://localhost:8081`. Check service status and startup logs with:

```powershell
docker compose ps
docker compose logs --tail=20 api api2
```

Compose networking uses service names (`db:5432` and `redis:6379`) between
containers. PostgreSQL data is kept in the named `postgres_data` volume.

## Run the Go API locally

The Go module and commands are in `server/`. Install Go 1.26 or newer and have
PostgreSQL and Redis reachable from the host. The application reads
`DATABASE_URL` and `REDIS_ADDR` from the process environment; it does not load
`.env` files.

PowerShell example (use your local connection values):

```powershell
$env:DATABASE_URL = "postgres://rtchat_app:YOUR_PASSWORD@localhost:5432/rtchat_dev?sslmode=disable"
$env:REDIS_ADDR = "localhost:6379"
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
and publishes a JSON event to Redis. Every API instance subscribes and forwards
events to its own connected clients in that channel. HTTP message create, edit,
and delete operations publish the same event types: `message.created`,
`message.updated`, and `message.deleted`. PostgreSQL remains the durable source
of truth; Redis Pub/Sub does not retain or replay missed events. See the
[WebSocket message flow](docs/websocket-message-flow.md).

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
