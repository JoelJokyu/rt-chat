# Database performance decisions

This note records decisions and measurements from the initial PostgreSQL
query-plan review and local HTTP load tests. Unless noted otherwise, results
are from `rtchat_test`, seeded with 100,000 messages across 10 channels. They
are directional measurements on one development machine, not production
capacity benchmarks.

## Message-history index

Add a partial index for active-message history:

```sql
CREATE INDEX messages_active_channel_history_idx
ON messages (channel_id, created_at DESC, id DESC)
WHERE deleted_at IS NULL;
```

The first-page query initially scanned 100,000 rows and sorted the matching
messages. With the index, PostgreSQL used an index scan and returned the page
without that large sort. The observed local execution time fell from roughly
16 ms to roughly 0.2 ms. Keyset pagination using `(created_at, id)` also used
the index.

The partial index measured about 3.7 MB, versus about 3.9 MB for a comparable
full index. The full index did not show a meaningful read-time advantage in
these tests.

### Tradeoff accepted

The partial index excludes soft-deleted messages. It therefore does not cover
lookups for all messages during a rare server/channel cascade delete; those
lookups may be slower than with a full index. Message-history reads are the
more important workload for this project, so this tradeoff is accepted for
now and can be revisited if measurements show deletion performance matters.

## Message-list authorization query

`ListMessages` now performs the membership check and page fetch in one SQL
statement instead of two database round trips. It distinguishes a non-member
(so the service can return `403 Forbidden`) from a member whose channel has no
messages (an empty page). The implemented query uses a materialized
access-check CTE and a lateral message query gated by that check.

This is an application query change, not a schema change. It reduces a
round trip but makes result scanning more involved: when a member has no
messages, the query still returns the membership result with NULL message
columns. The repository handles that case explicitly. The query was exercised
with both a member and a non-member in `rtchat_test`; the non-member result
avoids fetching the message page.

## Other query checks

Representative server, channel, and member-list plans were reviewed. Their
small result sets did not provide evidence for adding indexes at this stage.
Avoid adding indexes to those paths without a larger representative dataset
or a measured bottleneck.

## Measurement limits and next step

The seed dataset and local `EXPLAIN (ANALYZE, BUFFERS)` checks helped compare
query shapes and index behavior. A single-row insert check was too small to
support a reliable write-performance conclusion; no additional index choice
is based on that result.

## Local HTTP load-test observations (2026-10-13)

The load generator ran on the same machine as the API and PostgreSQL, using
loopback HTTP and `rtchat_test`. The varied scenario rotated among 10 seeded
channels and paged through message history. It used 20 closed-loop workers
(each worker sends another request after its previous response completes), for
30 seconds, with a page limit of 50.

| Workload | Requests | Throughput | Average latency | p50 | p95 | Outcomes |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| History reads only | 234,467 | 7,814.3 req/s | 2.266 ms | 2.102 ms | 4.179 ms | 200: 234,467; errors: 0 |
| 90% history reads / 10% message creation | 276,651 | 9,221.2 req/s | 1.972 ms overall; reads 1.974 ms; writes 1.956 ms | 1.811 ms overall | 3.553 ms overall; reads 3.556 ms; writes 3.535 ms | 200: 248,994; 201: 27,657; errors: 0 |

The write run removed its 27,657 load-test messages after completion. The 90/10
split is a deliberately selected read-heavy experiment, not a measured
real-world traffic ratio. These two runs are useful baseline observations,
not a controlled A/B comparison: cache state, host activity, and run-to-run
variation can affect results. In particular, the higher total request rate in
the mixed run does not establish that writes make reads faster or are free.

These numbers measure this local API/database/load-generator setup, not
production capacity. They do not cover WebSocket delivery, external network
latency, multiple API instances, or a realistic user population. The earlier
`hot`-scenario latency figures are not directly comparable because that
version measured latency at response headers rather than after the response
body was consumed.

The migration is captured in `server/migrations/008_add_active_message_history_index.sql`,
and the application query change is implemented. The next useful step is to
preserve these baselines and choose a specific additional workload only if it
answers a question (for example, WebSocket fan-out); avoid escalating worker
counts without a target or a bottleneck hypothesis.
