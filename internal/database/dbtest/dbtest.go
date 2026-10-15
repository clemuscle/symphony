// Package dbtest ouvre la base PostgreSQL jetable des tests, seulement si
// SYMPHONY_TEST_DB=1 (avec DB_HOST, DB_PORT… ; la CI en fournit une) : les
// tests qui en dépendent sont sautés sinon.
//
//	docker run -d --name pg-test -e POSTGRES_USER=symphony -e POSTGRES_PASSWORD=symphony123 \
//	  -e POSTGRES_DB=symphony -p 55432:5432 postgres:16-alpine
//	SYMPHONY_TEST_DB=1 DB_PORT=55432 go test ./...
package dbtest

import (
	"os"
	"testing"

	"github.com/clemuscle/symphony/internal/database"
)

// Open renvoie la base de test, migrée
func Open(t *testing.T) *database.DB {
	t.Helper()
	if os.Getenv("SYMPHONY_TEST_DB") != "1" {
		t.Skip("SYMPHONY_TEST_DB=1 pour tester contre PostgreSQL")
	}
	db, err := database.Connect()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
