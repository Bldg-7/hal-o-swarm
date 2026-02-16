package supervisor

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type AuditEntry struct {
	ID         string
	Timestamp  time.Time
	Actor      string
	Action     string
	Target     string
	Args       string
	Result     string
	Error      string
	DurationMs int
	IPAddress  string
}

type AuditLogger struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewAuditLogger(db *sql.DB, logger *zap.Logger) *AuditLogger {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AuditLogger{db: db, logger: logger}
}

func (a *AuditLogger) LogCommand(cmd Command, result *CommandResult, actor string, ipAddr string, duration time.Duration) {
	if a.db == nil {
		return
	}

	entry := AuditEntry{
		ID:         uuid.NewString(),
		Timestamp:  time.Now().UTC(),
		Actor:      actor,
		Action:     string(cmd.Type),
		Target:     cmd.Target.Project,
		Args:       SanitizeArgs(cmd.Args),
		DurationMs: int(duration.Milliseconds()),
		IPAddress:  ipAddr,
	}

	if cmd.Target.NodeID != "" && entry.Target == "" {
		entry.Target = cmd.Target.NodeID
	}
	if entry.Target == "" {
		entry.Target = "unknown"
	}

	if result != nil {
		entry.Result = string(result.Status)
		entry.Error = result.Error
	} else {
		entry.Result = "failure"
	}

	if err := a.insertEntry(entry); err != nil {
		a.logger.Warn("failed to write audit log entry",
			zap.String("action", entry.Action),
			zap.Error(err),
		)
	}
}

func (a *AuditLogger) insertEntry(entry AuditEntry) error {
	_, err := a.db.Exec(`
		INSERT INTO audit_log (id, timestamp, actor, action, target, args, result, error, duration_ms, ip_address)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, entry.Timestamp.Format(time.RFC3339Nano), entry.Actor, entry.Action,
		entry.Target, entry.Args, entry.Result, entry.Error, entry.DurationMs, entry.IPAddress)
	return err
}

func (a *AuditLogger) QueryByActor(actor string, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	return a.queryEntries("SELECT id, timestamp, actor, action, target, args, result, error, duration_ms, ip_address FROM audit_log WHERE actor = ? ORDER BY timestamp DESC LIMIT ?", actor, limit)
}

func (a *AuditLogger) QueryByAction(action string, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	return a.queryEntries("SELECT id, timestamp, actor, action, target, args, result, error, duration_ms, ip_address FROM audit_log WHERE action = ? ORDER BY timestamp DESC LIMIT ?", action, limit)
}

func (a *AuditLogger) PurgeOlderThan(retentionDays int) (int64, error) {
	if a.db == nil {
		return 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339Nano)
	result, err := a.db.Exec("DELETE FROM audit_log WHERE timestamp < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (a *AuditLogger) queryEntries(query string, args ...interface{}) ([]AuditEntry, error) {
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts, errStr, argsStr, ipAddr sql.NullString
		if err := rows.Scan(&e.ID, &ts, &e.Actor, &e.Action, &e.Target, &argsStr, &e.Result, &errStr, &e.DurationMs, &ipAddr); err != nil {
			return nil, err
		}
		if ts.Valid {
			if t, err := time.Parse(time.RFC3339Nano, ts.String); err == nil {
				e.Timestamp = t
			}
		}
		if errStr.Valid {
			e.Error = errStr.String
		}
		if argsStr.Valid {
			e.Args = argsStr.String
		}
		if ipAddr.Valid {
			e.IPAddress = ipAddr.String
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
