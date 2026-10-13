// Command loadtest sends concurrent, authenticated message API requests to a
// running API. It only accepts a *_test database and cleans up its test writes.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rtchat/internal/auth"

	"github.com/jackc/pgx/v5/pgxpool"
)

const seedServerName = "__rtchat_seedperf_v1__"

type loadTestCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

type loadTestPage struct {
	NextCursor *loadTestCursor `json:"next_cursor"`
}

type requestResult struct {
	status         int
	expectedStatus int
	isWrite        bool
	latency        time.Duration
	err            error
}

func main() {
	if err := run(); err != nil {
		log.Printf("Load test failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	baseURL := flag.String("url", "http://localhost:8080", "base URL of the API")
	workers := flag.Int("workers", 5, "number of concurrent request workers")
	duration := flag.Duration("duration", 30*time.Second, "how long to send requests")
	limit := flag.Int("limit", 50, "message page size per request")
	scenario := flag.String("scenario", "varied", "read pattern: varied (rotate channels/pages) or hot (repeat first page)")
	writePercent := flag.Int("write-percent", 0, "percentage of requests that create a message (0-100)")
	flag.Parse()

	if *workers < 1 || *workers > 500 {
		return errors.New("-workers must be between 1 and 500")
	}
	if *duration <= 0 {
		return errors.New("-duration must be positive")
	}
	if *limit < 1 || *limit > 100 {
		return errors.New("-limit must be between 1 and 100")
	}
	if *scenario != "hot" && *scenario != "varied" {
		return errors.New("-scenario must be either hot or varied")
	}
	if *writePercent < 0 || *writePercent > 100 {
		return errors.New("-write-percent must be between 0 and 100")
	}

	databaseURL := strings.TrimSpace(getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("TEST_DATABASE_URL is not set; refusing to use DATABASE_URL")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse TEST_DATABASE_URL: %w", err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		return fmt.Errorf("refusing database %q; database name must end in _test", config.ConnConfig.Database)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

	pool, err := pgxpool.NewWithConfig(startupCtx, config)
	if err != nil {
		return fmt.Errorf("connect to test database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("ping test database: %w", err)
	}

	var serverID, userID int64
	err = pool.QueryRow(startupCtx, `
		SELECT s.id, m.user_id
		FROM servers AS s
		JOIN memberships AS m ON m.server_id = s.id
		WHERE s.name = $1
		  AND m.role = 'owner'
		LIMIT 1`, seedServerName).Scan(&serverID, &userID)
	if err != nil {
		return fmt.Errorf("find seedperf server/member (run cmd/seedperf first): %w", err)
	}

	channelRows, err := pool.Query(startupCtx, `
		SELECT id
		FROM channels
		WHERE server_id = $1
		  AND name LIKE 'perf-channel-%'
		ORDER BY name`, serverID)
	if err != nil {
		return fmt.Errorf("list seedperf channels: %w", err)
	}
	channelIDs := make([]int64, 0)
	for channelRows.Next() {
		var channelID int64
		if err := channelRows.Scan(&channelID); err != nil {
			channelRows.Close()
			return fmt.Errorf("scan seedperf channel: %w", err)
		}
		channelIDs = append(channelIDs, channelID)
	}
	if err := channelRows.Err(); err != nil {
		channelRows.Close()
		return fmt.Errorf("read seedperf channels: %w", err)
	}
	channelRows.Close()
	if len(channelIDs) == 0 {
		return errors.New("seedperf server has no channels; run cmd/seedperf first")
	}

	sessionService := auth.NewSessionService(auth.NewSessionRepository(pool))
	rawToken, err := sessionService.CreateSession(startupCtx, userID, time.Now().Add(*duration+2*time.Minute))
	if err != nil {
		return fmt.Errorf("create temporary test session: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, cleanupErr := pool.Exec(cleanupCtx,
			"UPDATE sessions SET revoked_at = NOW() WHERE token_hash = $1 AND revoked_at IS NULL",
			auth.HashToken(rawToken),
		)
		if cleanupErr != nil {
			log.Printf("warning: could not revoke temporary load-test session: %v", cleanupErr)
		}
	}()

	runPrefix, err := newRunPrefix()
	if err != nil {
		return fmt.Errorf("create load-test marker: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		commandTag, cleanupErr := pool.Exec(cleanupCtx,
			"DELETE FROM messages WHERE channel_id = ANY($1::bigint[]) AND content LIKE $2",
			channelIDs,
			runPrefix+"%",
		)
		if cleanupErr != nil {
			log.Printf("warning: could not remove load-test messages: %v", cleanupErr)
			return
		}
		if commandTag.RowsAffected() > 0 {
			log.Printf("Removed %d load-test messages", commandTag.RowsAffected())
		}
	}()

	base := strings.TrimRight(*baseURL, "/")
	warmupEndpoint := messageHistoryURL(base, channelIDs[0], *limit, nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = *workers
	transport.MaxConnsPerHost = *workers
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	defer transport.CloseIdleConnections()

	// A single authenticated warm-up request catches bad setup before the run.
	warmup, err := newRequest(http.MethodGet, warmupEndpoint, rawToken, nil)
	if err != nil {
		return err
	}
	warmupResponse, err := client.Do(warmup)
	if err != nil {
		return fmt.Errorf("warm-up request failed: %w", err)
	}
	_, _ = io.Copy(io.Discard, warmupResponse.Body)
	_ = warmupResponse.Body.Close()
	if warmupResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("warm-up returned HTTP %d; check that the API uses the same test database", warmupResponse.StatusCode)
	}

	log.Printf("Target: %s/channels/{seeded-channel}/messages", base)
	log.Printf("Test database: %s | scenario: %s | channels: %d | workers: %d | duration: %s | writes: %d%%", config.ConnConfig.Database, *scenario, len(channelIDs), *workers, *duration, *writePercent)

	results := make(chan requestResult)
	start := make(chan struct{})
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignals()
	workCtx, cancelWork := context.WithTimeout(signalCtx, *duration)
	defer cancelWork()

	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			requestNumber := 0
			cursors := make(map[int64]*loadTestCursor)
			for {
				if workCtx.Err() != nil {
					return
				}

				sequence := requestNumber
				channelID := channelIDs[0]
				var cursor *loadTestCursor
				if *scenario == "varied" {
					channelID = channelIDs[(workerID+sequence)%len(channelIDs)]
					cursor = cursors[channelID]
				}
				requestNumber++
				isWrite := *writePercent > 0 && (sequence+1)*(*writePercent)/100 > sequence*(*writePercent)/100
				method := http.MethodGet
				endpoint := messageHistoryURL(base, channelID, *limit, cursor)
				var requestBody []byte
				expectedStatus := http.StatusOK
				if isWrite {
					method = http.MethodPost
					endpoint = messageCreateURL(base, channelID)
					expectedStatus = http.StatusCreated
					content := fmt.Sprintf("%sworker-%d-request-%d", runPrefix, workerID, sequence)
					var marshalErr error
					requestBody, marshalErr = json.Marshal(struct {
						Content string `json:"content"`
					}{Content: content})
					if marshalErr != nil {
						results <- requestResult{isWrite: isWrite, err: marshalErr}
						continue
					}
				}

				request, requestErr := newRequest(method, endpoint, rawToken, requestBody)
				if requestErr != nil {
					results <- requestResult{isWrite: isWrite, err: requestErr}
					continue
				}
				requestStart := time.Now()
				response, requestErr := client.Do(request)
				if requestErr != nil {
					results <- requestResult{expectedStatus: expectedStatus, isWrite: isWrite, latency: time.Since(requestStart), err: requestErr}
					continue
				}

				var bodyErr error
				var latency time.Duration
				if !isWrite && *scenario == "varied" && response.StatusCode == http.StatusOK {
					var body []byte
					body, bodyErr = io.ReadAll(response.Body)
					latency = time.Since(requestStart)
					if bodyErr == nil {
						var page loadTestPage
						bodyErr = json.Unmarshal(body, &page)
						if bodyErr == nil {
							if page.NextCursor == nil {
								delete(cursors, channelID)
							} else {
								cursors[channelID] = page.NextCursor
							}
						}
					}
				} else {
					_, bodyErr = io.Copy(io.Discard, response.Body)
					latency = time.Since(requestStart)
				}
				_ = response.Body.Close()
				if bodyErr != nil {
					results <- requestResult{status: response.StatusCode, expectedStatus: expectedStatus, isWrite: isWrite, latency: latency, err: bodyErr}
					continue
				}
				results <- requestResult{status: response.StatusCode, expectedStatus: expectedStatus, isWrite: isWrite, latency: latency}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	startedAt := time.Now()
	close(start)

	latencies := make([]time.Duration, 0)
	readLatencies := make([]time.Duration, 0)
	writeLatencies := make([]time.Duration, 0)
	statusCounts := make(map[int]int)
	var requestErrors int
	var completedRequests int
	var unexpectedStatuses int
	for result := range results {
		// A very fast response can measure as 0 at the timer's resolution;
		// still include it so response totals and latency sample counts agree.
		if result.status != 0 || result.latency > 0 {
			completedRequests++
			latencies = append(latencies, result.latency)
			if result.isWrite {
				writeLatencies = append(writeLatencies, result.latency)
			} else {
				readLatencies = append(readLatencies, result.latency)
			}
		}
		if result.status != 0 {
			statusCounts[result.status]++
			if result.status != result.expectedStatus {
				unexpectedStatuses++
			}
		}
		if result.err != nil {
			requestErrors++
			continue
		}
	}
	actualDuration := time.Since(startedAt)

	if completedRequests == 0 {
		return errors.New("no HTTP requests completed")
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var totalLatency time.Duration
	for _, latency := range latencies {
		totalLatency += latency
	}
	requestsPerSecond := float64(completedRequests) / actualDuration.Seconds()

	log.Printf("Completed: %d requests in %s (%.1f requests/sec)", completedRequests, actualDuration.Round(time.Millisecond), requestsPerSecond)
	logLatencySummary("all", latencies, totalLatency)
	logLatencySummary("reads", readLatencies, sumDurations(readLatencies))
	logLatencySummary("writes", writeLatencies, sumDurations(writeLatencies))
	log.Printf("Operations: reads=%d writes=%d | HTTP statuses: %v | request errors: %d", statusCounts[http.StatusOK], statusCounts[http.StatusCreated], statusCounts, requestErrors)

	if requestErrors > 0 {
		return fmt.Errorf("load run had %d request errors", requestErrors)
	}
	if unexpectedStatuses > 0 {
		return fmt.Errorf("load run had %d responses with an unexpected status", unexpectedStatuses)
	}
	return nil
}

func newRequest(method, endpoint, token string, body []byte) (*http.Request, error) {
	request, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	request.AddCookie(&http.Cookie{Name: "session_id", Value: token})
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

func messageCreateURL(base string, channelID int64) string {
	return fmt.Sprintf("%s/channels/%d/messages", base, channelID)
}

func newRunPrefix() (string, error) {
	var randomID [8]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return "", err
	}
	return "rtchat-loadtest-" + hex.EncodeToString(randomID[:]) + "-", nil
}

func messageHistoryURL(base string, channelID int64, limit int, cursor *loadTestCursor) string {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	if cursor != nil {
		query.Set("before_created_at", cursor.CreatedAt.Format(time.RFC3339Nano))
		query.Set("before_id", strconv.FormatInt(cursor.ID, 10))
	}
	return fmt.Sprintf("%s/channels/%d/messages?%s", base, channelID, query.Encode())
}

func percentile(sortedValues []time.Duration, fraction float64) time.Duration {
	index := int(math.Ceil(fraction*float64(len(sortedValues)))) - 1
	if index < 0 {
		index = 0
	}
	return sortedValues[index]
}

func sumDurations(values []time.Duration) time.Duration {
	var total time.Duration
	for _, value := range values {
		total += value
	}
	return total
}

func logLatencySummary(label string, values []time.Duration, total time.Duration) {
	if len(values) == 0 {
		log.Printf("Latency %s: no completed requests", label)
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	log.Printf("Latency %s: avg=%s p50=%s p95=%s max=%s", label, (total / time.Duration(len(values))).Round(time.Microsecond), percentile(values, 0.50).Round(time.Microsecond), percentile(values, 0.95).Round(time.Microsecond), values[len(values)-1].Round(time.Microsecond))
}

func getenv(key string) string {
	return strings.TrimSpace(strings.Trim(os.Getenv(key), "\""))
}
