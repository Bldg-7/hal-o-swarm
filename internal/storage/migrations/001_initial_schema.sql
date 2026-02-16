-- Initial schema for Hal-o-swarm
-- Tables: events, sessions, nodes, costs, command_idempotency

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    type TEXT NOT NULL,
    data TEXT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE INDEX IF NOT EXISTS idx_events_session_ts ON events(session_id, timestamp);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    project TEXT NOT NULL,
    status TEXT NOT NULL,
    tokens INTEGER DEFAULT 0,
    cost REAL DEFAULT 0.0,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (node_id) REFERENCES nodes(id)
);

CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);

CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    status TEXT NOT NULL,
    last_heartbeat DATETIME,
    connected_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS costs (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    date DATE NOT NULL,
    tokens INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0.0
);

CREATE INDEX IF NOT EXISTS idx_costs_date ON costs(date);

CREATE TABLE IF NOT EXISTS command_idempotency (
    key_hash TEXT PRIMARY KEY,
    command_id TEXT NOT NULL,
    result TEXT,
    expires_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_idempotency_key ON command_idempotency(key_hash);
