package storage

import "database/sql"

type Event struct {
	ID        string
	SessionID string
	Type      string
	Data      string
	Timestamp string
}

type Session struct {
	ID        string
	NodeID    string
	Project   string
	Status    string
	Tokens    int
	Cost      float64
	StartedAt string
}

type Node struct {
	ID            string
	Hostname      string
	Status        string
	LastHeartbeat string
	ConnectedAt   string
}

type Cost struct {
	ID       string
	Provider string
	Model    string
	Date     string
	Tokens   int
	CostUSD  float64
}

type CommandIdempotency struct {
	KeyHash   string
	CommandID string
	Result    string
	ExpiresAt string
}

type Storage struct {
	db *sql.DB
}

func NewStorage(db *sql.DB) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Close() error {
	return s.db.Close()
}
