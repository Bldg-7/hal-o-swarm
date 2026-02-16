package supervisor

import (
	"testing"
)

func TestMetricsInitialization(t *testing.T) {
	m := InitMetrics()
	if m == nil {
		t.Fatal("InitMetrics returned nil")
	}
}

func TestRecordCommand(t *testing.T) {
	m := InitMetrics()
	m.RecordCommand("test_command", "success")
	m.RecordCommand("test_command", "failure")
}

func TestRecordCommandDuration(t *testing.T) {
	m := InitMetrics()
	m.RecordCommandDuration("test_command", 1.5)
	m.RecordCommandDuration("test_command", 2.3)
}

func TestRecordEvent(t *testing.T) {
	m := InitMetrics()
	m.RecordEvent("test_event")
	m.RecordEvent("test_event")
}

func TestRecordEventProcessingDuration(t *testing.T) {
	m := InitMetrics()
	m.RecordEventProcessingDuration("test_event", 0.5)
	m.RecordEventProcessingDuration("test_event", 1.2)
}

func TestRecordConnection(t *testing.T) {
	m := InitMetrics()
	m.RecordConnection("accepted")
	m.RecordConnection("rejected")
}

func TestSetActiveConnections(t *testing.T) {
	m := InitMetrics()
	m.SetActiveConnections(5)
	m.SetActiveConnections(10)
}

func TestSetActiveSessions(t *testing.T) {
	m := InitMetrics()
	m.SetActiveSessions("running", 3)
	m.SetActiveSessions("idle", 2)
}

func TestSetOnlineNodes(t *testing.T) {
	m := InitMetrics()
	m.SetOnlineNodes(4)
	m.SetOnlineNodes(5)
}

func TestRecordError(t *testing.T) {
	m := InitMetrics()
	m.RecordError("dispatcher", "timeout")
	m.RecordError("hub", "connection_failed")
}

func TestMetricsNilSafety(t *testing.T) {
	var m *Metrics
	m.RecordCommand("test", "success")
	m.RecordCommandDuration("test", 1.0)
	m.RecordEvent("test")
	m.RecordEventProcessingDuration("test", 1.0)
	m.RecordConnection("accepted")
	m.SetActiveConnections(5)
	m.SetActiveSessions("running", 3)
	m.SetOnlineNodes(4)
	m.RecordError("test", "error")
}

func TestGetMetrics(t *testing.T) {
	m1 := GetMetrics()
	m2 := GetMetrics()
	if m1 != m2 {
		t.Fatal("GetMetrics should return the same instance")
	}
}
