package database

import (
	"fmt"
	"time"
)

type AuditEntry struct {
	ID        int       `json:"id"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Details   string    `json:"details"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (db *DB) Log(action, resource, details, userID string) error {
	_, err := db.Exec(`
		INSERT INTO audit_log (action, resource, details, user_id)
		VALUES ($1,$2,$3,$4)`,
		action, resource, details, userID)
	return err
}

func (db *DB) ListAudit(limit int, project, actor string) ([]AuditEntry, error) {
	if limit == 0 {
		limit = 100
	}
	q := `SELECT id, action, resource, details, user_id, created_at FROM audit_log WHERE 1=1`
	args := []any{}
	idx := 1
	if project != "" {
		q += fmt.Sprintf(` AND resource ILIKE $%d`, idx)
		args = append(args, "%"+project+"%")
		idx++
	}
	if actor != "" {
		q += fmt.Sprintf(` AND user_id = $%d`, idx)
		args = append(args, actor)
		idx++
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, idx)
	args = append(args, limit)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		rows.Scan(&e.ID, &e.Action, &e.Resource, &e.Details, &e.UserID, &e.CreatedAt)
		entries = append(entries, e)
	}
	return entries, nil
}
