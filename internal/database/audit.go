package database

import "time"

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

func (db *DB) ListAudit(limit int) ([]AuditEntry, error) {
	if limit == 0 {
		limit = 50
	}
	rows, err := db.Query(`
		SELECT id, action, resource, details, user_id, created_at
		FROM audit_log ORDER BY created_at DESC LIMIT $1`, limit)
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
