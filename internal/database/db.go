package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type DB struct {
	*sql.DB
}

func Connect() (*DB, error) {
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		log.Fatal("DB_PASSWORD manquant")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=%s",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_NAME", "symphony"),
		getEnv("DB_USER", "symphony"),
		dbPassword,
		getEnv("DB_SSLMODE", "disable"),
	)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	const maxAttempts = 10
	const retryDelay = 2 * time.Second

	var pingErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pingErr = db.Ping()
		if pingErr == nil {
			log.Println("✅ PostgreSQL connecté")
			return &DB{db}, nil
		}
		log.Printf("⏳ PostgreSQL pas encore prêt (tentative %d/%d): %v", attempt, maxAttempts, pingErr)
		if attempt < maxAttempts {
			time.Sleep(retryDelay)
		}
	}

	return nil, fmt.Errorf("ping db after %d attempts: %w", maxAttempts, pingErr)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
