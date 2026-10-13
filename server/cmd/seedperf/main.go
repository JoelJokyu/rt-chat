// Command seedperf creates a repeatable synthetic dataset for query-plan and
// load-testing exercises. It only connects to databases whose name ends in
// "_test" and only replaces rows carrying this command's dedicated marker.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	seedServerName = "__rtchat_seedperf_v1__"
	seedEmailBase  = "rtchat-seedperf-v1-"
	maxMessages    = 2_000_000
)

func main() {
	messageCount := flag.Int("messages", 100_000, "number of synthetic messages to create (max 2,000,000)")
	channelCount := flag.Int("channels", 10, "number of channels to create")
	memberCount := flag.Int("members", 10, "number of server members to create, including the owner")
	cleanupOnly := flag.Bool("cleanup", false, "remove this command's previously seeded data")
	flag.Parse()

	if !*cleanupOnly {
		if *messageCount < 1 || *messageCount > maxMessages {
			log.Fatalf("-messages must be between 1 and %d", maxMessages)
		}
		if *channelCount < 1 || *channelCount > 1_000 {
			log.Fatal("-channels must be between 1 and 1000")
		}
		if *memberCount < 1 || *memberCount > 10_000 {
			log.Fatal("-members must be between 1 and 10000")
		}
	}

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("TEST_DATABASE_URL is not set; refusing to use DATABASE_URL")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		log.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		log.Fatalf("refusing to use database %q; database name must end in _test", config.ConnConfig.Database)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		log.Fatalf("connect to test database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping test database: %v", err)
	}

	if *cleanupOnly {
		if err := cleanupSeed(ctx, pool); err != nil {
			log.Fatalf("clean up seeded data: %v", err)
		}
		log.Printf("Removed seedperf data from %s", config.ConnConfig.Database)
		return
	}

	serverID, channelIDs, memberIDs, err := seed(ctx, pool, *messageCount, *channelCount, *memberCount)
	if err != nil {
		log.Fatalf("seed performance data: %v", err)
	}

	log.Printf("Seeded performance dataset in %s", config.ConnConfig.Database)
	log.Printf("server_id=%d channels=%d members=%d messages=%d", serverID, len(channelIDs), len(memberIDs), *messageCount)
	log.Printf("Use channel_id=%d and user_id=%d in your message-history EXPLAIN query", channelIDs[0], memberIDs[0])
	log.Printf("Remove this dataset with: go run ./cmd/seedperf -cleanup")
}

func seed(ctx context.Context, pool *pgxpool.Pool, messageCount, channelCount, memberCount int) (int64, []int64, []int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("begin seed transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := deleteSeedRows(ctx, tx); err != nil {
		return 0, nil, nil, fmt.Errorf("replace previous seed: %w", err)
	}

	var serverID int64
	if err := tx.QueryRow(ctx,
		"INSERT INTO servers (name) VALUES ($1) RETURNING id",
		seedServerName,
	).Scan(&serverID); err != nil {
		return 0, nil, nil, fmt.Errorf("insert server: %w", err)
	}

	memberIDs := make([]int64, memberCount)
	for i := range memberIDs {
		displayName := fmt.Sprintf("PerfUser%04d", i+1)
		email := fmt.Sprintf("%s%04d@example.test", seedEmailBase, i+1)
		if err := tx.QueryRow(ctx,
			"INSERT INTO users (display_name, email, password_hash) VALUES ($1, $2, $3) RETURNING id",
			displayName, email, "seedperf-not-a-login-password",
		).Scan(&memberIDs[i]); err != nil {
			return 0, nil, nil, fmt.Errorf("insert user %d: %w", i+1, err)
		}

		role := "member"
		if i == 0 {
			role = "owner"
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO memberships (server_id, user_id, role) VALUES ($1, $2, $3)",
			serverID, memberIDs[i], role,
		); err != nil {
			return 0, nil, nil, fmt.Errorf("insert membership for user %d: %w", i+1, err)
		}
	}

	channelIDs := make([]int64, channelCount)
	for i := range channelIDs {
		channelName := fmt.Sprintf("perf-channel-%04d", i+1)
		if err := tx.QueryRow(ctx,
			"INSERT INTO channels (server_id, name) VALUES ($1, $2) RETURNING id",
			serverID, channelName,
		).Scan(&channelIDs[i]); err != nil {
			return 0, nil, nil, fmt.Errorf("insert channel %d: %w", i+1, err)
		}
	}

	const insertMessages = `
		WITH seed_values AS (
			SELECT $2::bigint[] AS channel_ids, $3::bigint[] AS member_ids
		)
		INSERT INTO messages (channel_id, author_id, content, created_at, deleted_at)
		SELECT seed_values.channel_ids[((generated.i - 1) % cardinality(seed_values.channel_ids)) + 1],
		       seed_values.member_ids[((generated.i - 1) % cardinality(seed_values.member_ids)) + 1],
		       'Synthetic performance message ' || generated.i,
		       now() - (generated.i * interval '1 second'),
		       CASE
		           WHEN (((generated.i - 1) / cardinality(seed_values.channel_ids)) + 1) % 20 = 0 THEN now()
		           ELSE NULL
		       END
		FROM generate_series(1, $1::integer) AS generated(i)
		CROSS JOIN seed_values`
	if _, err := tx.Exec(ctx, insertMessages, messageCount, channelIDs, memberIDs); err != nil {
		return 0, nil, nil, fmt.Errorf("insert %d messages: %w", messageCount, err)
	}

	for _, table := range []string{"messages", "channels", "memberships"} {
		if _, err := tx.Exec(ctx, "ANALYZE "+table); err != nil {
			return 0, nil, nil, fmt.Errorf("update %s table statistics: %w", table, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, nil, fmt.Errorf("commit seed transaction: %w", err)
	}

	return serverID, channelIDs, memberIDs, nil
}

func cleanupSeed(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cleanup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := deleteSeedRows(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func deleteSeedRows(ctx context.Context, tx pgx.Tx) error {
	// The seed's server cascades to its channels, messages, and memberships.
	if _, err := tx.Exec(ctx, "DELETE FROM servers WHERE name = $1", seedServerName); err != nil {
		return fmt.Errorf("delete seed server: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM users WHERE email LIKE $1", seedEmailBase+"%@example.test"); err != nil {
		return fmt.Errorf("delete seed users: %w", err)
	}
	return nil
}
