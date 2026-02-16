package supervisor

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type mockDiscordSession struct {
	mu sync.Mutex

	openCalled  bool
	closeCalled bool

	respondCalls  []respondCall
	followupCalls []followupCall

	registeredCmds []*discordgo.ApplicationCommand
	deletedCmdIDs  []string

	handler func(s *discordgo.Session, i *discordgo.InteractionCreate)
	state   *discordgo.State
}

type respondCall struct {
	Interaction *discordgo.Interaction
	Response    *discordgo.InteractionResponse
}

type followupCall struct {
	Interaction *discordgo.Interaction
	Params      *discordgo.WebhookParams
}

func (m *mockDiscordSession) AddHandler(handler interface{}) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := handler.(func(s *discordgo.Session, i *discordgo.InteractionCreate)); ok {
		m.handler = h
	}
	return func() {}
}

func (m *mockDiscordSession) Open() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openCalled = true
	return nil
}

func (m *mockDiscordSession) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

func (m *mockDiscordSession) ApplicationCommandCreate(appID, guildID string, cmd *discordgo.ApplicationCommand, _ ...discordgo.RequestOption) (*discordgo.ApplicationCommand, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	registered := *cmd
	registered.ID = fmt.Sprintf("cmd-%s", cmd.Name)
	m.registeredCmds = append(m.registeredCmds, &registered)
	return &registered, nil
}

func (m *mockDiscordSession) ApplicationCommandDelete(appID, guildID, cmdID string, _ ...discordgo.RequestOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedCmdIDs = append(m.deletedCmdIDs, cmdID)
	return nil
}

func (m *mockDiscordSession) InteractionRespond(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse, _ ...discordgo.RequestOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.respondCalls = append(m.respondCalls, respondCall{Interaction: interaction, Response: resp})
	return nil
}

func (m *mockDiscordSession) FollowupMessageCreate(interaction *discordgo.Interaction, wait bool, params *discordgo.WebhookParams, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.followupCalls = append(m.followupCalls, followupCall{Interaction: interaction, Params: params})
	return &discordgo.Message{ID: "msg-1"}, nil
}

func (m *mockDiscordSession) State() *discordgo.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *mockDiscordSession) lastFollowupEmbed() *discordgo.MessageEmbed {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.followupCalls) == 0 {
		return nil
	}
	last := m.followupCalls[len(m.followupCalls)-1]
	if len(last.Params.Embeds) == 0 {
		return nil
	}
	return last.Params.Embeds[0]
}

func (m *mockDiscordSession) lastRespondType() discordgo.InteractionResponseType {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.respondCalls) == 0 {
		return 0
	}
	return m.respondCalls[len(m.respondCalls)-1].Response.Type
}

func newTestState(appID string) *discordgo.State {
	s := &discordgo.State{}
	s.User = &discordgo.User{ID: appID}
	return s
}

func newTestDiscordBot(t *testing.T) (*DiscordBot, *mockDiscordSession, *CommandDispatcher) {
	t.Helper()

	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)

	if err := registry.Register(NodeEntry{ID: "node-1", Hostname: "host-1"}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	if err := tracker.AddSession(TrackedSession{
		SessionID:   "sess-1",
		NodeID:      "node-1",
		Project:     "myproject",
		Status:      SessionStatusRunning,
		Model:       "claude-4",
		CurrentTask: "implement feature",
		TokenUsage:  TokenUsage{Total: 5000},
		SessionCost: 1.50,
		StartedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("add session: %v", err)
	}

	var dispatcher *CommandDispatcher
	transport := &mockCommandTransport{}
	transport.onSend = func(nodeID string, cmd Command) {
		go func(commandID string) {
			time.Sleep(5 * time.Millisecond)
			dispatcher.HandleCommandResult(CommandResult{
				CommandID: commandID,
				Status:    CommandStatusSuccess,
				Output:    "ok",
				Timestamp: time.Now().UTC(),
			})
		}(cmd.CommandID)
	}
	dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	mock := &mockDiscordSession{
		state: newTestState("app-123"),
	}

	bot := NewDiscordBotWithSession(mock, "guild-1", dispatcher, nil, tracker, logger)
	return bot, mock, dispatcher
}

func simulateInteraction(bot *DiscordBot, name string, options []*discordgo.ApplicationCommandInteractionDataOption) {
	bot.handleInteraction(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    name,
				Options: options,
			},
		},
	})
}

func TestDiscordCommandsHappy(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "status", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "project", Type: discordgo.ApplicationCommandOptionString, Value: "myproject"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Status: myproject" {
			t.Fatalf("expected title 'Status: myproject', got %q", embed.Title)
		}
		if embed.Color != colorSuccess {
			t.Fatalf("expected success color, got %d", embed.Color)
		}
		if mock.lastRespondType() != discordgo.InteractionResponseDeferredChannelMessageWithSource {
			t.Fatal("expected deferred response type")
		}
	})

	t.Run("nodes", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "nodes", nil)

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Agent Nodes" {
			t.Fatalf("expected title 'Agent Nodes', got %q", embed.Title)
		}
		if embed.Color != colorInfo {
			t.Fatalf("expected info color, got %d", embed.Color)
		}
	})

	t.Run("logs", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "logs", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "session_id", Type: discordgo.ApplicationCommandOptionString, Value: "sess-1"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Logs: sess-1" {
			t.Fatalf("expected title 'Logs: sess-1', got %q", embed.Title)
		}
		if len(embed.Fields) != 3 {
			t.Fatalf("expected 3 fields, got %d", len(embed.Fields))
		}
	})

	t.Run("resume", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "resume", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "project", Type: discordgo.ApplicationCommandOptionString, Value: "myproject"},
			{Name: "message", Type: discordgo.ApplicationCommandOptionString, Value: "continue"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Resume: myproject" {
			t.Fatalf("expected title 'Resume: myproject', got %q", embed.Title)
		}
		if embed.Color != colorSuccess {
			t.Fatalf("expected success color, got %d", embed.Color)
		}
	})

	t.Run("inject", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "inject", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "session_id", Type: discordgo.ApplicationCommandOptionString, Value: "sess-1"},
			{Name: "message", Type: discordgo.ApplicationCommandOptionString, Value: "fix the bug"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Inject: sess-1" {
			t.Fatalf("expected title 'Inject: sess-1', got %q", embed.Title)
		}
	})

	t.Run("restart", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "restart", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "session_id", Type: discordgo.ApplicationCommandOptionString, Value: "sess-1"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Restart: sess-1" {
			t.Fatalf("expected title 'Restart: sess-1', got %q", embed.Title)
		}
	})

	t.Run("kill", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "kill", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "session_id", Type: discordgo.ApplicationCommandOptionString, Value: "sess-1"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Kill: sess-1" {
			t.Fatalf("expected title 'Kill: sess-1', got %q", embed.Title)
		}
	})

	t.Run("start", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "start", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "project", Type: discordgo.ApplicationCommandOptionString, Value: "myproject"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Start: myproject" {
			t.Fatalf("expected title 'Start: myproject', got %q", embed.Title)
		}
	})

	t.Run("cost", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "cost", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "period", Type: discordgo.ApplicationCommandOptionString, Value: "today"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Cost Summary" {
			t.Fatalf("expected title 'Cost Summary', got %q", embed.Title)
		}
		if len(embed.Fields) != 3 {
			t.Fatalf("expected 3 fields, got %d", len(embed.Fields))
		}
	})
}

func TestDiscordCommandsError(t *testing.T) {
	t.Run("invalid_project_resume", func(t *testing.T) {
		db := setupSupervisorTestDB(t)
		logger := zap.NewNop()
		registry := NewNodeRegistry(db, logger)
		tracker := NewSessionTracker(db, logger)
		transport := &mockCommandTransport{}
		dispatcher := NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

		mock := &mockDiscordSession{
			state: newTestState("app-123"),
		}
		bot := NewDiscordBotWithSession(mock, "guild-1", dispatcher, nil, tracker, logger)

		simulateInteraction(bot, "resume", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "project", Type: discordgo.ApplicationCommandOptionString, Value: "nonexistent"},
			{Name: "message", Type: discordgo.ApplicationCommandOptionString, Value: "hello"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Color != colorFailure {
			t.Fatalf("expected failure color (%d), got %d", colorFailure, embed.Color)
		}
		if embed.Description == "" {
			t.Fatal("expected error description")
		}
	})

	t.Run("missing_required_arg", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "status", nil)

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Validation Error" {
			t.Fatalf("expected 'Validation Error' title, got %q", embed.Title)
		}
		if embed.Color != colorError {
			t.Fatalf("expected error color, got %d", embed.Color)
		}
	})

	t.Run("session_not_found_logs", func(t *testing.T) {
		bot, mock, _ := newTestDiscordBot(t)
		simulateInteraction(bot, "logs", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "session_id", Type: discordgo.ApplicationCommandOptionString, Value: "nonexistent-sess"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Title != "Session Not Found" {
			t.Fatalf("expected 'Session Not Found' title, got %q", embed.Title)
		}
		if embed.Color != colorError {
			t.Fatalf("expected error color, got %d", embed.Color)
		}
	})

	t.Run("offline_node_kill", func(t *testing.T) {
		db := setupSupervisorTestDB(t)
		logger := zap.NewNop()
		registry := NewNodeRegistry(db, logger)
		tracker := NewSessionTracker(db, logger)

		if err := registry.Register(NodeEntry{ID: "node-off", Hostname: "host-off"}); err != nil {
			t.Fatalf("register: %v", err)
		}
		if err := registry.MarkOffline("node-off"); err != nil {
			t.Fatalf("mark offline: %v", err)
		}
		if err := tracker.AddSession(TrackedSession{
			SessionID: "sess-off",
			NodeID:    "node-off",
			Project:   "proj-off",
			Status:    SessionStatusUnreachable,
			StartedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("add session: %v", err)
		}

		transport := &mockCommandTransport{}
		dispatcher := NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)
		mock := &mockDiscordSession{
			state: newTestState("app-123"),
		}
		bot := NewDiscordBotWithSession(mock, "guild-1", dispatcher, nil, tracker, logger)

		simulateInteraction(bot, "kill", []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "session_id", Type: discordgo.ApplicationCommandOptionString, Value: "sess-off"},
		})

		embed := mock.lastFollowupEmbed()
		if embed == nil {
			t.Fatal("expected followup embed")
		}
		if embed.Color != colorFailure {
			t.Fatalf("expected failure color, got %d", embed.Color)
		}
		if embed.Description == "" {
			t.Fatal("expected sanitized error description")
		}
	})
}

func TestDiscordBotStartStop(t *testing.T) {
	bot, mock, _ := newTestDiscordBot(t)

	if err := bot.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	mock.mu.Lock()
	if !mock.openCalled {
		t.Fatal("expected Open() to be called")
	}
	if len(mock.registeredCmds) != 9 {
		t.Fatalf("expected 9 registered commands, got %d", len(mock.registeredCmds))
	}
	mock.mu.Unlock()

	if err := bot.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	mock.mu.Lock()
	if !mock.closeCalled {
		t.Fatal("expected Close() to be called")
	}
	if len(mock.deletedCmdIDs) != 9 {
		t.Fatalf("expected 9 deleted commands, got %d", len(mock.deletedCmdIDs))
	}
	mock.mu.Unlock()
}

func TestDiscordSlashCommandDefinitions(t *testing.T) {
	cmds := slashCommands()
	if len(cmds) != 9 {
		t.Fatalf("expected 9 slash commands, got %d", len(cmds))
	}

	expected := map[string]bool{
		"status": true, "nodes": true, "logs": true,
		"resume": true, "inject": true, "restart": true,
		"kill": true, "start": true, "cost": true,
	}
	for _, cmd := range cmds {
		if !expected[cmd.Name] {
			t.Errorf("unexpected command: %s", cmd.Name)
		}
		if cmd.Description == "" {
			t.Errorf("command %s has empty description", cmd.Name)
		}
	}
}

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"node xyz is not connected", "not connected"},
		{"target node offline: abc", "offline"},
		{"command timed out after 30s", "timed out"},
		{"node foo command channel is saturated", "busy"},
		{"target node not found: bar", "not found"},
		{"some internal panic stack trace", "error occurred"},
	}

	for _, tt := range tests {
		result := sanitizeError(tt.input)
		if result == tt.input {
			t.Errorf("sanitizeError should not pass through raw error: %q", tt.input)
		}
	}
}

func TestCostDefaultPeriod(t *testing.T) {
	bot, mock, _ := newTestDiscordBot(t)
	simulateInteraction(bot, "cost", nil)

	embed := mock.lastFollowupEmbed()
	if embed == nil {
		t.Fatal("expected followup embed")
	}
	if embed.Title != "Cost Summary" {
		t.Fatalf("expected 'Cost Summary', got %q", embed.Title)
	}
}
