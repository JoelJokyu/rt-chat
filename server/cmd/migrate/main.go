package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	ctx := context.Background()

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("could not create database pool: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}

	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		log.Fatalf("could not create schema_migrations table: %v", err)
	}

	files, err := os.ReadDir("migrations")
	if err != nil {
		log.Fatalf("could not read migrations directory: %v", err)
	}

	for _, file := range files {
		if file.IsDir() || strings.ToLower(filepath.Ext(file.Name())) != ".sql" {
			continue
		}

		var alreadyApplied bool
		err := db.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
			file.Name(),
		).Scan(&alreadyApplied)
		if err != nil {
			log.Fatalf("could not check migration %s: %v", file.Name(), err)
		}

		if alreadyApplied {
			log.Printf("Skipping %s (already applied)", file.Name())
			continue
		}

		sql, err := os.ReadFile(filepath.Join("migrations", file.Name()))
		if err != nil {
			log.Fatalf("could not read migration %s: %v", file.Name(), err)
		}

		tx, err := db.Begin(ctx)
		if err != nil {
			log.Fatalf("could not begin migration %s: %v", file.Name(), err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("could not run migration %s: %v", file.Name(), err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			file.Name(),
		); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("could not record migration %s: %v", file.Name(), err)
		}

		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("could not commit migration %s: %v", file.Name(), err)
		}

		log.Printf("Applied %s", file.Name())
	}

	log.Println("Migrations complete")
}
