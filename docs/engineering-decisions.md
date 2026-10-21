# Engineering Decisions

This document captures the choices that most shape RT Chat's architecture and
behavior. Endpoint contracts, schema details, WebSocket flow, and performance
measurements are documented separately and linked below.

## 1. A modular monolith with explicit application layers

RT Chat runs as one Go API process, organized into domain packages. HTTP
handlers manage transport concerns, services express use cases and rules, and
repositories own PostgreSQL queries.

This keeps deployment and runtime simple while making the boundaries between
HTTP, application behavior, and persistence visible. The trade-off is some
additional package and call structure compared with putting logic directly in
handlers; multiple services or distributed infrastructure are not justified
by the current requirements.

The API uses Go's `net/http` rather than a framework. That keeps routing,
middleware, and request handling close to the standard library and avoids
adding a framework dependency for the current route set.

## 2. PostgreSQL is the durable source of truth

PostgreSQL stores accounts, sessions, servers, memberships, channels, join
codes, and complete message records. Messages are not transient WebSocket
payloads or IDs that point to another store. This makes HTTP history and
realtime delivery operate on the same durable data.

Schema changes are versioned as SQL migrations. `pgx`/`pgxpool` provides the
Go database access layer. The schema's foreign keys encode lifecycle rules:
server deletion cascades to its dependent data, while user deletion is
restricted when memberships, authored messages, or created join codes still
reference that user. There is no user-deletion API currently.

See the [ER diagram](database-er-diagram.md) for the schema and the
[API reference](api.md) for resource behavior.

## 3. Membership role is the single source of server permissions

The role (`member`, `admin`, or `owner`) lives on the membership row; ownership
is not duplicated in a `servers.owner_id` field. This avoids maintaining two
sources of truth for a user's server role.

A partial unique index enforces at most one owner membership per server.
Creating a server and its owner's membership happens in one transaction, so
the application creation path gives a new server its owner atomically. The
database constraint alone does not enforce that every server always has an
owner. Owner/admin/member capabilities and edge cases are detailed in the
[API reference](api.md).

## 4. Opaque database sessions instead of JWTs

Login creates a random session token and sends it in an HTTP-only cookie. Only
its SHA-256 hash is stored in PostgreSQL, along with expiry and revocation
state. Sessions expire after 72 hours, and logout revokes the current session.

This stateful design allows logout and revocation to take effect immediately.
The trade-off is a database lookup for authenticated requests and dependence
on session storage. The cookie's `Secure` attribute is currently disabled for
local HTTP development; production requires HTTPS and secure cookie settings.

Join codes use a related pattern: generate a random code, return the raw value
once, and store only its hash with expiry/revocation data. This prevents a
database read from directly exposing usable session or invite tokens.

## 5. Message history uses keyset pagination and a workload-specific index

History is ordered by `(created_at, id)` and paginated with a cursor using the
same tuple. Including the ID makes ordering deterministic when timestamps
match. The API fetches one extra row to determine whether another page exists.

A partial index covers active messages in channel-history order. It improves
the common history-read path without indexing soft-deleted rows; the trade-off
is that cascade deletion may not benefit from that index. This decision came
from local query-plan inspection and load testing, not a claim about
production capacity. The measurements and limitations are in
[database performance decisions](database-performance-decisions.md).

## 6. Persist messages first; use Redis Pub/Sub for cross-instance fan-out

For both WebSocket and HTTP writes, the message operation succeeds in
PostgreSQL before the corresponding event is published. The event contains the
persisted message ID and timestamp, and PostgreSQL remains the durable source
of truth. Redis Pub/Sub carries the event between API processes; each process
has its own in-memory hub that queues events to its connected clients by
channel. The publishing process also receives its event through its Redis
subscription, so producers do not separately broadcast to the local hub.

This was added after running two API instances showed that a process-local hub
cannot reach clients connected to another process. Redis is used for transient
fan-out, not as a message store or durable queue. Pub/Sub does not replay events
to disconnected subscribers. If publishing fails after the PostgreSQL write,
the application logs the failure; the message remains saved, but live clients
may not see the change until they fetch history. Reliable event delivery would
require a durable mechanism such as an outbox, which is outside the current
scope.

The full connection, persistence, broadcast, and disconnect sequence is in
the [WebSocket message flow](websocket-message-flow.md).

## Scope boundary

The current implementation can run multiple API instances with Redis-backed
WebSocket fan-out, but it is not a production deployment recipe. CORS, rate
limiting, deployment-specific HTTPS settings, and durable event delivery are
not implemented. Add these when a client or deployment requirement calls for
them, rather than treating them as already solved architecture.
