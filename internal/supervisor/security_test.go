package supervisor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"github.com/Bldg-7/hal-o-swarm/internal/storage"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

func generateSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", "127.0.0.1"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPath = filepath.Join(dir, "cert.pem")
	certFile, err := os.Create(certPath)
	if err != nil {
		t.Fatalf("create cert file: %v", err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("encode cert: %v", err)
	}
	certFile.Close()

	keyPath = filepath.Join(dir, "key.pem")
	keyFile, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatalf("encode key: %v", err)
	}
	keyFile.Close()

	return certPath, keyPath
}

func TestSecurityTLSConfigLoad(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateSelfSignedCert(t, dir)

	tlsCfg, err := LoadTLSConfig(config.TLSConfig{
		Enabled:  true,
		CertPath: certPath,
		KeyPath:  keyPath,
	})
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if tlsCfg == nil {
		t.Fatal("expected non-nil TLS config")
	}
	if tlsCfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("expected TLS 1.2 minimum, got %d", tlsCfg.MinVersion)
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
	}
}

func TestSecurityTLSConfigDisabled(t *testing.T) {
	tlsCfg, err := LoadTLSConfig(config.TLSConfig{Enabled: false})
	if err != nil {
		t.Fatalf("LoadTLSConfig disabled: %v", err)
	}
	if tlsCfg != nil {
		t.Error("expected nil TLS config when disabled")
	}
}

func TestSecurityTLSConfigInvalidCert(t *testing.T) {
	_, err := LoadTLSConfig(config.TLSConfig{
		Enabled:  true,
		CertPath: "/nonexistent/cert.pem",
		KeyPath:  "/nonexistent/key.pem",
	})
	if err == nil {
		t.Error("expected error for invalid cert paths")
	}
}

func TestSecurityOriginMatchExact(t *testing.T) {
	if !MatchOrigin("https://example.com", "https://example.com") {
		t.Error("exact match should pass")
	}
	if MatchOrigin("https://evil.com", "https://example.com") {
		t.Error("different domain should fail")
	}
}

func TestSecurityOriginMatchWildcardPort(t *testing.T) {
	if !MatchOrigin("http://localhost:3000", "http://localhost:*") {
		t.Error("wildcard port should match localhost:3000")
	}
	if !MatchOrigin("http://localhost:8080", "http://localhost:*") {
		t.Error("wildcard port should match localhost:8080")
	}
	if !MatchOrigin("http://localhost", "http://localhost:*") {
		t.Error("wildcard port should match localhost without port")
	}
	if MatchOrigin("http://evil.com:3000", "http://localhost:*") {
		t.Error("wildcard port should not match different host")
	}
}

func TestSecurityOriginMatchWildcardSubdomain(t *testing.T) {
	if !MatchOrigin("https://app.example.com", "https://*.example.com") {
		t.Error("wildcard subdomain should match app.example.com")
	}
	if !MatchOrigin("https://dev.example.com", "https://*.example.com") {
		t.Error("wildcard subdomain should match dev.example.com")
	}
	if MatchOrigin("https://example.com", "https://*.example.com") {
		t.Error("wildcard subdomain should not match bare domain")
	}
	if MatchOrigin("http://app.example.com", "https://*.example.com") {
		t.Error("wildcard subdomain should not match wrong scheme")
	}
}

func TestSecurityOriginMatchStar(t *testing.T) {
	if !MatchOrigin("https://anything.com", "*") {
		t.Error("star should match everything")
	}
}

func TestSecuritySanitizeArgs(t *testing.T) {
	args := map[string]interface{}{
		"project":    "my-app",
		"auth_token": "secret123",
		"password":   "hunter2",
		"api_key":    "sk-123",
		"prompt":     "hello world",
	}

	result := SanitizeArgs(args)

	if strings.Contains(result, "secret123") {
		t.Error("auth_token should be redacted")
	}
	if strings.Contains(result, "hunter2") {
		t.Error("password should be redacted")
	}
	if strings.Contains(result, "sk-123") {
		t.Error("api_key should be redacted")
	}
	if !strings.Contains(result, "my-app") {
		t.Error("project should not be redacted")
	}
	if !strings.Contains(result, "hello world") {
		t.Error("prompt should not be redacted")
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Error("should contain [REDACTED] markers")
	}
}

func TestSecuritySanitizeArgsNil(t *testing.T) {
	result := SanitizeArgs(nil)
	if result != "{}" {
		t.Errorf("nil args should return {}, got %s", result)
	}
}

func TestSecurityIsSecretKey(t *testing.T) {
	secrets := []string{"token", "password", "secret", "api_key", "AUTH_TOKEN", "my_credential"}
	for _, k := range secrets {
		if !IsSecretKey(k) {
			t.Errorf("%q should be detected as secret", k)
		}
	}

	safe := []string{"project", "name", "prompt", "session_id", "status"}
	for _, k := range safe {
		if IsSecretKey(k) {
			t.Errorf("%q should not be detected as secret", k)
		}
	}
}

func TestSecurityAudit(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()
	audit := NewAuditLogger(db, logger)

	cmd := Command{
		CommandID: "cmd-001",
		Type:      CommandTypeCreateSession,
		Target:    CommandTarget{Project: "my-project"},
		Args:      map[string]interface{}{"prompt": "build it"},
	}

	result := &CommandResult{
		CommandID: "cmd-001",
		Status:    CommandStatusSuccess,
		Timestamp: time.Now().UTC(),
	}

	audit.LogCommand(cmd, result, "node-1", "127.0.0.1", 500*time.Millisecond)

	entries, err := audit.QueryByActor("node-1", 10)
	if err != nil {
		t.Fatalf("QueryByActor: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Actor != "node-1" {
		t.Errorf("expected actor node-1, got %s", entry.Actor)
	}
	if entry.Action != "create_session" {
		t.Errorf("expected action create_session, got %s", entry.Action)
	}
	if entry.Target != "my-project" {
		t.Errorf("expected target my-project, got %s", entry.Target)
	}
	if entry.Result != "success" {
		t.Errorf("expected result success, got %s", entry.Result)
	}
	if entry.IPAddress != "127.0.0.1" {
		t.Errorf("expected IP 127.0.0.1, got %s", entry.IPAddress)
	}
	if entry.DurationMs != 500 {
		t.Errorf("expected 500ms duration, got %d", entry.DurationMs)
	}
}

func TestSecurityAuditSecretsRedacted(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()
	audit := NewAuditLogger(db, logger)

	cmd := Command{
		CommandID: "cmd-002",
		Type:      CommandTypePromptSession,
		Target:    CommandTarget{Project: "my-project"},
		Args: map[string]interface{}{
			"prompt":    "do things",
			"api_token": "super-secret-value",
		},
	}

	result := &CommandResult{
		CommandID: "cmd-002",
		Status:    CommandStatusSuccess,
		Timestamp: time.Now().UTC(),
	}

	audit.LogCommand(cmd, result, "node-1", "10.0.0.1", time.Second)

	entries, err := audit.QueryByAction("prompt_session", 10)
	if err != nil {
		t.Fatalf("QueryByAction: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if strings.Contains(entries[0].Args, "super-secret-value") {
		t.Error("secret should be redacted in audit log args")
	}
	if !strings.Contains(entries[0].Args, "[REDACTED]") {
		t.Error("args should contain [REDACTED]")
	}
	if !strings.Contains(entries[0].Args, "do things") {
		t.Error("non-secret args should be preserved")
	}
}

func TestSecurityAuditPurge(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()
	audit := NewAuditLogger(db, logger)

	cmd := Command{
		CommandID: "cmd-old",
		Type:      CommandTypeKillSession,
		Target:    CommandTarget{Project: "old-project"},
	}

	audit.LogCommand(cmd, &CommandResult{
		CommandID: "cmd-old",
		Status:    CommandStatusSuccess,
		Timestamp: time.Now().UTC(),
	}, "node-1", "10.0.0.1", time.Millisecond)

	deleted, err := audit.PurgeOlderThan(0)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	entries, err := audit.QueryByActor("node-1", 10)
	if err != nil {
		t.Fatalf("QueryByActor after purge: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after purge, got %d", len(entries))
	}
}

func TestSecurityRejectStrictOriginMissing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test-token", []string{"https://allowed.example.com"}, 30*time.Second, 3, zap.NewNop())
	hub.SetStrictOrigin(true)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer test-token")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err == nil {
		t.Fatal("expected dial to fail with missing origin in strict mode")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestSecurityRejectStrictOriginInvalid(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test-token", []string{"https://allowed.example.com"}, 30*time.Second, 3, zap.NewNop())
	hub.SetStrictOrigin(true)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer test-token")
	header.Set("Origin", "https://evil.example.com")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err == nil {
		t.Fatal("expected dial to fail with invalid origin in strict mode")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestSecurityAcceptStrictOriginValid(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test-token", []string{"https://allowed.example.com"}, 30*time.Second, 3, zap.NewNop())
	hub.SetStrictOrigin(true)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer test-token")
	header.Set("Origin", "https://allowed.example.com")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err != nil {
		t.Fatalf("dial should succeed with valid origin: %v", err)
	}
	defer conn.Close()

	select {
	case ev := <-hub.Events():
		if ev.Type != "node.online" {
			t.Errorf("expected node.online, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for node.online")
	}
}

func TestSecurityTokenRotation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "old-token-that-is-at-least-32-chars-long", nil, 30*time.Second, 3, zap.NewNop())
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer old-token-that-is-at-least-32-chars-long")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err != nil {
		t.Fatalf("dial with old token: %v", err)
	}

	select {
	case <-hub.Events():
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	newToken := "new-token-that-is-also-at-least-32-chars!"
	hub.UpdateAuthToken(newToken)

	header2 := http.Header{}
	header2.Set("Authorization", "Bearer old-token-that-is-at-least-32-chars-long")
	_, _, err = websocket.DefaultDialer.Dial(wsURL(server), header2)
	if err == nil {
		t.Fatal("old token should be rejected after rotation")
	}

	header3 := http.Header{}
	header3.Set("Authorization", "Bearer "+newToken)
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL(server), header3)
	if err != nil {
		t.Fatalf("new token should be accepted: %v", err)
	}
	defer conn2.Close()

	select {
	case ev := <-hub.Events():
		if ev.Type != "node.online" {
			t.Errorf("expected node.online, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for node.online with new token")
	}
}

func TestSecurityServerRotateTokenMinLength(t *testing.T) {
	cfg := &config.SupervisorConfig{}
	cfg.Server.Port = 9999
	cfg.Server.AuthToken = "initial-token-that-is-long-enough-32"
	cfg.Server.HeartbeatIntervalSec = 30
	cfg.Server.HeartbeatTimeoutCount = 3

	srv := NewServer(cfg, zap.NewNop())

	if err := srv.RotateToken("short"); err == nil {
		t.Error("expected error for short token")
	}

	longToken := "this-is-a-sufficiently-long-token-for-rotation"
	if err := srv.RotateToken(longToken); err != nil {
		t.Fatalf("RotateToken: %v", err)
	}
}

func TestSecurityStrictOriginWildcardPatterns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test-token", []string{"http://localhost:*", "https://*.example.com"}, 30*time.Second, 3, zap.NewNop())
	hub.SetStrictOrigin(true)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	tests := []struct {
		origin string
		expect bool
	}{
		{"http://localhost:3000", true},
		{"http://localhost:8080", true},
		{"https://app.example.com", true},
		{"https://dev.example.com", true},
		{"https://evil.com", false},
		{"http://not-localhost:3000", false},
	}

	for _, tc := range tests {
		header := http.Header{}
		header.Set("Authorization", "Bearer test-token")
		header.Set("Origin", tc.origin)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), header)
		if tc.expect {
			if err != nil {
				t.Errorf("origin %q should be accepted: %v", tc.origin, err)
				continue
			}
			select {
			case <-hub.Events():
			case <-time.After(time.Second):
			}
			conn.Close()
			time.Sleep(50 * time.Millisecond)
		} else {
			if err == nil {
				conn.Close()
				select {
				case <-hub.Events():
				case <-time.After(100 * time.Millisecond):
				}
				t.Errorf("origin %q should be rejected", tc.origin)
			}
		}
	}
}

func TestSecurityAuditNilDB(t *testing.T) {
	audit := NewAuditLogger(nil, zap.NewNop())
	cmd := Command{
		CommandID: "cmd-003",
		Type:      CommandTypeCreateSession,
		Target:    CommandTarget{Project: "p"},
	}
	audit.LogCommand(cmd, nil, "actor", "1.2.3.4", time.Second)
}

func TestSecurityConfigValidation(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateSelfSignedCert(t, dir)

	cfg := &config.SupervisorConfig{}
	cfg.Server.Port = 9999
	cfg.Server.HTTPPort = 0
	cfg.Server.AuthToken = "test-token"
	cfg.Server.HeartbeatIntervalSec = 30
	cfg.Server.HeartbeatTimeoutCount = 3
	cfg.Security.TLS.Enabled = true
	cfg.Security.TLS.CertPath = certPath
	cfg.Security.TLS.KeyPath = keyPath

	srv := NewServer(cfg, zap.NewNop())
	tlsCfg, err := LoadTLSConfig(cfg.Security.TLS)
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	srv.SetTLSConfig(tlsCfg)
	if srv.tlsConfig == nil {
		t.Error("TLS config should be set on server")
	}
}

func TestSecurityAuditQueryByAction(t *testing.T) {
	db := setupSupervisorTestDB(t)
	audit := NewAuditLogger(db, zap.NewNop())

	for i := 0; i < 3; i++ {
		cmd := Command{
			CommandID: "cmd-q-" + string(rune('a'+i)),
			Type:      CommandTypeKillSession,
			Target:    CommandTarget{NodeID: "node-x"},
		}
		audit.LogCommand(cmd, &CommandResult{
			CommandID: cmd.CommandID,
			Status:    CommandStatusSuccess,
			Timestamp: time.Now().UTC(),
		}, "admin", "10.0.0.1", time.Millisecond)
	}

	entries, err := audit.QueryByAction("kill_session", 10)
	if err != nil {
		t.Fatalf("QueryByAction: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestSecurityMigrationAuditTable(t *testing.T) {
	db := setupSupervisorTestDB(t)

	_, err := db.Exec("INSERT INTO audit_log (id, actor, action, target, result) VALUES ('test-1', 'actor-1', 'action-1', 'target-1', 'success')")
	if err != nil {
		t.Fatalf("insert into audit_log should succeed after migration: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&count); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestSecurityTLSServer(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateSelfSignedCert(t, dir)

	tlsCfg, err := LoadTLSConfig(config.TLSConfig{
		Enabled:  true,
		CertPath: certPath,
		KeyPath:  keyPath,
	})
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test-token", nil, 30*time.Second, 3, zap.NewNop())
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/agent", hub.ServeWS)

	server := httptest.NewUnstartedServer(mux)
	server.TLS = tlsCfg
	server.StartTLS()
	defer server.Close()

	wsURL := "wss" + strings.TrimPrefix(server.URL, "https") + "/ws/agent"

	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer test-token")
	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("WSS dial: %v", err)
	}
	defer conn.Close()

	select {
	case ev := <-hub.Events():
		if ev.Type != "node.online" {
			t.Errorf("expected node.online, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for node.online over WSS")
	}
}

func TestSecurityRejectForgedTokenNoStateMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "valid-token", nil, 30*time.Second, 3, zap.NewNop())
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	initialCount := hub.ClientCount()

	header := http.Header{}
	header.Set("Authorization", "Bearer forged-token")
	_, _, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err == nil {
		t.Fatal("forged token should be rejected")
	}

	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != initialCount {
		t.Errorf("client count should not change on rejected auth: was %d, now %d", initialCount, hub.ClientCount())
	}

	select {
	case ev := <-hub.Events():
		t.Errorf("no events should be emitted for rejected auth, got %s", ev.Type)
	default:
	}
}

func TestSecurityAuditFailedCommand(t *testing.T) {
	db := setupSupervisorTestDB(t)
	audit := NewAuditLogger(db, zap.NewNop())

	cmd := Command{
		CommandID: "cmd-fail",
		Type:      CommandTypeKillSession,
		Target:    CommandTarget{Project: "failing-project"},
	}

	result := &CommandResult{
		CommandID: "cmd-fail",
		Status:    CommandStatusFailure,
		Error:     "node not found",
		Timestamp: time.Now().UTC(),
	}

	audit.LogCommand(cmd, result, "node-1", "192.168.1.1", 100*time.Millisecond)

	entries, err := audit.QueryByActor("node-1", 10)
	if err != nil {
		t.Fatalf("QueryByActor: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Result != "failure" {
		t.Errorf("expected result failure, got %s", entries[0].Result)
	}
	if entries[0].Error != "node not found" {
		t.Errorf("expected error 'node not found', got %s", entries[0].Error)
	}
}

func TestSanitizeArgsRecursive(t *testing.T) {
	args := map[string]interface{}{
		"project": "my-app",
		"config": map[string]interface{}{
			"region":     "us-east-1",
			"account_id": "12345",
			"connection": map[string]interface{}{
				"api_key": "sk-deep-nested-key",
				"secret":  "deep-secret-value",
				"host":    "db.example.com",
			},
			"auth_token": "level2-token",
		},
		"items": []interface{}{
			"plain-string",
			map[string]interface{}{
				"name":     "item1",
				"password": "item-password-leak",
			},
		},
		"password": "top-level-pw",
	}

	result := SanitizeArgs(args)

	if strings.Contains(result, "top-level-pw") {
		t.Error("top-level password should be redacted")
	}
	if strings.Contains(result, "level2-token") {
		t.Error("nested auth_token should be redacted")
	}
	if strings.Contains(result, "sk-deep-nested-key") {
		t.Error("deeply nested api_key should be redacted")
	}
	if strings.Contains(result, "deep-secret-value") {
		t.Error("deeply nested secret should be redacted")
	}
	if strings.Contains(result, "item-password-leak") {
		t.Error("password inside array element should be redacted")
	}

	if !strings.Contains(result, "my-app") {
		t.Error("top-level project should be preserved")
	}
	if !strings.Contains(result, "us-east-1") {
		t.Error("nested region should be preserved")
	}
	if !strings.Contains(result, "12345") {
		t.Error("nested account_id should be preserved")
	}
	if !strings.Contains(result, "db.example.com") {
		t.Error("deeply nested non-secret host should be preserved")
	}
	if !strings.Contains(result, "plain-string") {
		t.Error("plain string in array should be preserved")
	}
	if !strings.Contains(result, "item1") {
		t.Error("non-secret key in array map should be preserved")
	}
}

func TestAuditNoPlainSecret(t *testing.T) {
	db := setupSupervisorTestDB(t)
	audit := NewAuditLogger(db, zap.NewNop())

	cmd := Command{
		CommandID: "cmd-nested-secret",
		Type:      CommandTypeCreateSession,
		Target:    CommandTarget{Project: "secure-project"},
		Args: map[string]interface{}{
			"prompt": "deploy it",
			"env": map[string]interface{}{
				"region":     "eu-west-1",
				"api_secret": "plaintext-api-secret-DO-NOT-LEAK",
				"nested": map[string]interface{}{
					"password":   "deeply-nested-password-DO-NOT-LEAK",
					"cluster_id": "cluster-42",
				},
			},
			"providers": []interface{}{
				map[string]interface{}{
					"name":       "anthropic",
					"auth_token": "sk-ant-DO-NOT-LEAK",
				},
			},
		},
	}

	result := &CommandResult{
		CommandID: "cmd-nested-secret",
		Status:    CommandStatusSuccess,
		Timestamp: time.Now().UTC(),
	}

	audit.LogCommand(cmd, result, "deployer", "10.0.0.5", 200*time.Millisecond)

	entries, err := audit.QueryByActor("deployer", 10)
	if err != nil {
		t.Fatalf("QueryByActor: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	storedArgs := entries[0].Args

	leaks := []string{
		"plaintext-api-secret-DO-NOT-LEAK",
		"deeply-nested-password-DO-NOT-LEAK",
		"sk-ant-DO-NOT-LEAK",
	}
	for _, leak := range leaks {
		if strings.Contains(storedArgs, leak) {
			t.Errorf("audit DB contains plaintext secret %q", leak)
		}
	}

	if !strings.Contains(storedArgs, "deploy it") {
		t.Error("prompt should be preserved in audit")
	}
	if !strings.Contains(storedArgs, "eu-west-1") {
		t.Error("region should be preserved in audit")
	}
	if !strings.Contains(storedArgs, "cluster-42") {
		t.Error("cluster_id should be preserved in audit")
	}
	if !strings.Contains(storedArgs, "anthropic") {
		t.Error("provider name should be preserved in audit")
	}
	if !strings.Contains(storedArgs, "[REDACTED]") {
		t.Error("should contain [REDACTED] markers")
	}
}

func TestAuditCredentialPushRedaction(t *testing.T) {
	db := setupSupervisorTestDB(t)
	audit := NewAuditLogger(db, zap.NewNop())

	cmd := Command{
		CommandID: "cmd-cred-push-redact",
		Type:      CommandTypeCredentialPush,
		Target:    CommandTarget{NodeID: "node-target-1"},
		Args: map[string]interface{}{
			"env_vars": map[string]interface{}{
				"ANTHROPIC_API_KEY":     "sk-ant-secret-DO-NOT-LEAK",
				"OPENAI_API_KEY":        "sk-openai-secret-DO-NOT-LEAK",
				"DATABASE_PASSWORD":     "db-pass-DO-NOT-LEAK",
				"AWS_SECRET_ACCESS_KEY": "aws-secret-DO-NOT-LEAK",
			},
			"version": 1,
		},
	}

	result := &CommandResult{
		CommandID: "cmd-cred-push-redact",
		Status:    CommandStatusSuccess,
		Timestamp: time.Now().UTC(),
	}

	audit.LogCommand(cmd, result, "api", "10.0.0.1", 150*time.Millisecond)

	entries, err := audit.QueryByAction("credential_push", 10)
	if err != nil {
		t.Fatalf("QueryByAction: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	storedArgs := entries[0].Args

	leaks := []string{
		"sk-ant-secret-DO-NOT-LEAK",
		"sk-openai-secret-DO-NOT-LEAK",
		"db-pass-DO-NOT-LEAK",
		"aws-secret-DO-NOT-LEAK",
	}
	for _, leak := range leaks {
		if strings.Contains(storedArgs, leak) {
			t.Errorf("audit args contain plaintext secret %q", leak)
		}
	}

	if !strings.Contains(storedArgs, "[REDACTED]") {
		t.Error("audit args should contain [REDACTED] markers")
	}
	if !strings.Contains(storedArgs, "env_vars") {
		t.Error("env_vars key should be preserved in audit args")
	}
}

func TestAuditCredentialPushMetadata(t *testing.T) {
	db := setupSupervisorTestDB(t)
	audit := NewAuditLogger(db, zap.NewNop())

	cmd := Command{
		CommandID: "cmd-cred-push-meta",
		Type:      CommandTypeCredentialPush,
		Target:    CommandTarget{NodeID: "node-push-target"},
		Args: map[string]interface{}{
			"env_vars": map[string]interface{}{
				"API_KEY": "some-value",
			},
			"version": 2,
		},
	}

	t.Run("success", func(t *testing.T) {
		result := &CommandResult{
			CommandID: "cmd-cred-push-meta",
			Status:    CommandStatusSuccess,
			Timestamp: time.Now().UTC(),
		}

		audit.LogCommand(cmd, result, "ops-admin", "192.168.1.100", 200*time.Millisecond)

		entries, err := audit.QueryByAction("credential_push", 10)
		if err != nil {
			t.Fatalf("QueryByAction: %v", err)
		}
		if len(entries) == 0 {
			t.Fatal("expected at least 1 audit entry")
		}

		entry := entries[0]
		if entry.Action != "credential_push" {
			t.Errorf("expected action credential_push, got %s", entry.Action)
		}
		if entry.Target != "node-push-target" {
			t.Errorf("expected target node-push-target, got %s", entry.Target)
		}
		if entry.Result != "success" {
			t.Errorf("expected result success, got %s", entry.Result)
		}
		if entry.Actor != "ops-admin" {
			t.Errorf("expected actor ops-admin, got %s", entry.Actor)
		}
		if entry.IPAddress != "192.168.1.100" {
			t.Errorf("expected IP 192.168.1.100, got %s", entry.IPAddress)
		}
		if entry.DurationMs != 200 {
			t.Errorf("expected 200ms duration, got %d", entry.DurationMs)
		}
	})

	t.Run("failure", func(t *testing.T) {
		failCmd := cmd
		failCmd.CommandID = "cmd-cred-push-fail"

		result := &CommandResult{
			CommandID: "cmd-cred-push-fail",
			Status:    CommandStatusFailure,
			Error:     "node unreachable",
			Timestamp: time.Now().UTC(),
		}

		audit.LogCommand(failCmd, result, "api", "10.0.0.5", 50*time.Millisecond)

		entries, err := audit.QueryByActor("api", 10)
		if err != nil {
			t.Fatalf("QueryByActor: %v", err)
		}

		var found *AuditEntry
		for i := range entries {
			if entries[i].Result == "failure" && entries[i].Action == "credential_push" {
				found = &entries[i]
				break
			}
		}
		if found == nil {
			t.Fatal("expected a failure audit entry for credential_push")
		}
		if found.Error != "node unreachable" {
			t.Errorf("expected error 'node unreachable', got %s", found.Error)
		}
	})
}

// Suppress unused import warning — needed for sqlite driver registration.
var _ = storage.NewMigrationRunner
