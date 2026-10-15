package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/lib/pq"
)

//go:embed migrations/001_init.sql
var migration001 []byte

//go:embed migrations/002_upgrade_legacy_schema.sql
var migration002 []byte

//go:embed migrations/003_tasks.sql
var migration003 []byte

//go:embed migrations/004_project_team.sql
var migration004 []byte

type DB struct {
	*sql.DB
}

func Connect() (*DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_NAME", "symphony"),
		getEnv("DB_USER", "symphony"),
		getEnv("DB_PASSWORD", "symphony123"),
	)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	// La base peut démarrer en même temps que Symphony (Kubernetes, compose) :
	// on l'attend une minute avant d'abandonner
	deadline := time.Now().Add(time.Minute)
	for {
		err := db.Ping()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("ping: %w", err)
		}
		slog.Warn("PostgreSQL pas encore joignable, nouvel essai dans 2 s", "error", err)
		time.Sleep(2 * time.Second)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	slog.Info("PostgreSQL connecté")
	return &DB{db}, nil
}

func (db *DB) Migrate() error {
	migrations := []struct {
		version string
		sql     []byte
	}{
		{"001", migration001},
		{"002", migration002},
		{"003", migration003},
		{"004", migration004},
	}

	// Une seule migration à la fois : deux instances (ou deux tests) qui
	// démarrent ensemble sur une base neuve créeraient les mêmes tables en
	// même temps. Verrou consultatif PostgreSQL, tenu par une connexion dédiée.
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLock); err != nil {
		return fmt.Errorf("migrations: verrou: %w", err)
	}
	defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrationLock)

	for _, m := range migrations {
		var exists bool
		err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, m.version).Scan(&exists)
		// Si la table n'existe pas encore, on ignore l'erreur et on applique
		if err != nil || !exists {
			if _, err := conn.ExecContext(ctx, string(m.sql)); err != nil {
				return fmt.Errorf("migration %s: %w", m.version, err)
			}
			conn.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING`, m.version)
			slog.Info("migration appliquée", "version", m.version)
		}
	}
	return nil
}

// migrationLock : clé du verrou consultatif des migrations
const migrationLock = 7_171_100

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
