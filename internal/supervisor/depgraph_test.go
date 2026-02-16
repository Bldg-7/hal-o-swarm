package supervisor

import (
	"database/sql"
	"testing"

	"go.uber.org/zap"
)

func TestNewDependencyGraphValidGraph(t *testing.T) {
	config := map[string]DependencyConfig{
		"ai-os-l0":       {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-l1":       {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-l2":       {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-launcher": {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-rom":      {DependsOn: []string{"ai-os-l0", "ai-os-l1", "ai-os-l2", "ai-os-launcher"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	if dg == nil {
		t.Fatal("DependencyGraph is nil")
	}
}

func TestNewDependencyGraphEmptyConfig(t *testing.T) {
	dg, err := NewDependencyGraph(nil)
	if err != nil {
		t.Fatalf("NewDependencyGraph with nil config failed: %v", err)
	}

	if dg == nil {
		t.Fatal("DependencyGraph is nil")
	}
}

func TestCycleDetectionSimple(t *testing.T) {
	config := map[string]DependencyConfig{
		"A": {DependsOn: []string{"B"}},
		"B": {DependsOn: []string{"A"}},
	}

	_, err := NewDependencyGraph(config)
	if err == nil {
		t.Fatal("Expected cycle detection error, got nil")
	}

	if err.Error() != "cycle detected: A -> B -> A" && err.Error() != "cycle detected: B -> A -> B" {
		t.Fatalf("Unexpected error message: %v", err)
	}
}

func TestCycleDetectionComplex(t *testing.T) {
	config := map[string]DependencyConfig{
		"A": {DependsOn: []string{"B"}},
		"B": {DependsOn: []string{"C"}},
		"C": {DependsOn: []string{"A"}},
	}

	_, err := NewDependencyGraph(config)
	if err == nil {
		t.Fatal("Expected cycle detection error, got nil")
	}

	errMsg := err.Error()
	if errMsg != "cycle detected: A -> B -> C -> A" && errMsg != "cycle detected: B -> C -> A -> B" && errMsg != "cycle detected: C -> A -> B -> C" {
		t.Fatalf("Unexpected error message: %v", err)
	}
}

func TestGetDependencies(t *testing.T) {
	config := map[string]DependencyConfig{
		"ai-os-rom": {DependsOn: []string{"ai-os-l0", "ai-os-l1", "ai-os-l2"}},
		"ai-os-l0":  {DependsOn: []string{"ai-os-interfaces"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	deps := dg.GetDependencies("ai-os-rom")
	if len(deps) != 3 {
		t.Fatalf("Expected 3 dependencies, got %d", len(deps))
	}

	expected := map[string]bool{"ai-os-l0": true, "ai-os-l1": true, "ai-os-l2": true}
	for _, dep := range deps {
		if !expected[dep] {
			t.Fatalf("Unexpected dependency: %s", dep)
		}
	}
}

func TestGetDependents(t *testing.T) {
	config := map[string]DependencyConfig{
		"ai-os-l0":  {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-l1":  {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-rom": {DependsOn: []string{"ai-os-l0", "ai-os-l1"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	dependents := dg.GetDependents("ai-os-interfaces")
	if len(dependents) != 2 {
		t.Fatalf("Expected 2 dependents, got %d", len(dependents))
	}

	expected := map[string]bool{"ai-os-l0": true, "ai-os-l1": true}
	for _, dep := range dependents {
		if !expected[dep] {
			t.Fatalf("Unexpected dependent: %s", dep)
		}
	}
}

func TestGetDependentsNone(t *testing.T) {
	config := map[string]DependencyConfig{
		"ai-os-l0": {DependsOn: []string{"ai-os-interfaces"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	dependents := dg.GetDependents("ai-os-l0")
	if len(dependents) != 0 {
		t.Fatalf("Expected 0 dependents, got %d", len(dependents))
	}
}

func TestTriggerDependentsEmptyProject(t *testing.T) {
	config := map[string]DependencyConfig{
		"A": {DependsOn: []string{"B"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	_, err = dg.TriggerDependents("", nil)
	if err == nil {
		t.Fatal("Expected error for empty project name")
	}
}

func TestTriggerDependentsNoDependent(t *testing.T) {
	config := map[string]DependencyConfig{
		"A": {DependsOn: []string{"B"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	ready, err := dg.TriggerDependents("A", nil)
	if err != nil {
		t.Fatalf("TriggerDependents failed: %v", err)
	}

	if len(ready) != 0 {
		t.Fatalf("Expected 0 ready projects, got %d", len(ready))
	}
}

func TestTriggerDependentsWithTracker(t *testing.T) {
	config := map[string]DependencyConfig{
		"ai-os-l0":  {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-l1":  {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-rom": {DependsOn: []string{"ai-os-l0", "ai-os-l1"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	db := setupTestDB(t)
	defer db.Close()

	tracker := NewSessionTracker(db, zap.NewNop())

	tracker.AddSession(TrackedSession{
		SessionID: "sess-1",
		NodeID:    "node-1",
		Project:   "ai-os-interfaces",
		Status:    SessionStatusRunning,
	})

	ready, err := dg.TriggerDependents("ai-os-interfaces", tracker)
	if err != nil {
		t.Fatalf("TriggerDependents failed: %v", err)
	}

	if len(ready) != 2 {
		t.Fatalf("Expected 2 ready projects, got %d: %v", len(ready), ready)
	}

	expected := map[string]bool{"ai-os-l0": true, "ai-os-l1": true}
	for _, proj := range ready {
		if !expected[proj] {
			t.Fatalf("Unexpected ready project: %s", proj)
		}
	}
}

func TestTriggerDependentsPartialCompletion(t *testing.T) {
	config := map[string]DependencyConfig{
		"ai-os-l0":  {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-l1":  {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-rom": {DependsOn: []string{"ai-os-l0", "ai-os-l1"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	db := setupTestDB(t)
	defer db.Close()

	tracker := NewSessionTracker(db, zap.NewNop())

	tracker.AddSession(TrackedSession{
		SessionID: "sess-1",
		NodeID:    "node-1",
		Project:   "ai-os-interfaces",
		Status:    SessionStatusRunning,
	})

	tracker.AddSession(TrackedSession{
		SessionID: "sess-2",
		NodeID:    "node-1",
		Project:   "ai-os-l0",
		Status:    SessionStatusRunning,
	})

	ready, err := dg.TriggerDependents("ai-os-l0", tracker)
	if err != nil {
		t.Fatalf("TriggerDependents failed: %v", err)
	}

	if len(ready) != 0 {
		t.Fatalf("Expected 0 ready projects (ai-os-rom needs both l0 and l1), got %d: %v", len(ready), ready)
	}
}

func TestTriggerDependentsAllDepsComplete(t *testing.T) {
	config := map[string]DependencyConfig{
		"ai-os-l0":  {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-l1":  {DependsOn: []string{"ai-os-interfaces"}},
		"ai-os-rom": {DependsOn: []string{"ai-os-l0", "ai-os-l1"}},
	}

	dg, err := NewDependencyGraph(config)
	if err != nil {
		t.Fatalf("NewDependencyGraph failed: %v", err)
	}

	db := setupTestDB(t)
	defer db.Close()

	tracker := NewSessionTracker(db, zap.NewNop())

	tracker.AddSession(TrackedSession{
		SessionID: "sess-1",
		NodeID:    "node-1",
		Project:   "ai-os-interfaces",
		Status:    SessionStatusRunning,
	})

	tracker.AddSession(TrackedSession{
		SessionID: "sess-2",
		NodeID:    "node-1",
		Project:   "ai-os-l0",
		Status:    SessionStatusRunning,
	})

	tracker.AddSession(TrackedSession{
		SessionID: "sess-3",
		NodeID:    "node-1",
		Project:   "ai-os-l1",
		Status:    SessionStatusRunning,
	})

	ready, err := dg.TriggerDependents("ai-os-l1", tracker)
	if err != nil {
		t.Fatalf("TriggerDependents failed: %v", err)
	}

	if len(ready) != 1 {
		t.Fatalf("Expected 1 ready project (ai-os-rom), got %d: %v", len(ready), ready)
	}

	if ready[0] != "ai-os-rom" {
		t.Fatalf("Expected ai-os-rom to be ready, got %s", ready[0])
	}
}

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	schema := `
	CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		node_id TEXT NOT NULL,
		project TEXT NOT NULL,
		status TEXT NOT NULL,
		tokens INTEGER DEFAULT 0,
		cost REAL DEFAULT 0,
		started_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("Failed to create test schema: %v", err)
	}

	return db
}
