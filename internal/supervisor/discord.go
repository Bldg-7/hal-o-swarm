package supervisor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

const (
	colorSuccess = 0x00CC66
	colorFailure = 0xCC3333
	colorTimeout = 0xFF9900
	colorInfo    = 0x3399FF
	colorError   = 0xCC3333
)

// DiscordSession abstracts the discordgo.Session methods used by DiscordBot,
// enabling mock-based testing without real Discord API calls.
type DiscordSession interface {
	AddHandler(handler interface{}) func()
	Open() error
	Close() error
	ApplicationCommandCreate(appID string, guildID string, cmd *discordgo.ApplicationCommand, options ...discordgo.RequestOption) (*discordgo.ApplicationCommand, error)
	ApplicationCommandDelete(appID string, guildID string, cmdID string, options ...discordgo.RequestOption) error
	InteractionRespond(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse, options ...discordgo.RequestOption) error
	FollowupMessageCreate(interaction *discordgo.Interaction, wait bool, params *discordgo.WebhookParams, options ...discordgo.RequestOption) (*discordgo.Message, error)
	State() *discordgo.State
}

type realDiscordSession struct {
	s *discordgo.Session
}

func (r *realDiscordSession) AddHandler(handler interface{}) func() {
	return r.s.AddHandler(handler)
}

func (r *realDiscordSession) Open() error {
	return r.s.Open()
}

func (r *realDiscordSession) Close() error {
	return r.s.Close()
}

func (r *realDiscordSession) ApplicationCommandCreate(appID, guildID string, cmd *discordgo.ApplicationCommand, options ...discordgo.RequestOption) (*discordgo.ApplicationCommand, error) {
	return r.s.ApplicationCommandCreate(appID, guildID, cmd, options...)
}

func (r *realDiscordSession) ApplicationCommandDelete(appID, guildID, cmdID string, options ...discordgo.RequestOption) error {
	return r.s.ApplicationCommandDelete(appID, guildID, cmdID, options...)
}

func (r *realDiscordSession) InteractionRespond(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse, options ...discordgo.RequestOption) error {
	return r.s.InteractionRespond(interaction, resp, options...)
}

func (r *realDiscordSession) FollowupMessageCreate(interaction *discordgo.Interaction, wait bool, params *discordgo.WebhookParams, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	return r.s.FollowupMessageCreate(interaction, wait, params, options...)
}

func (r *realDiscordSession) State() *discordgo.State {
	return r.s.State
}

// DiscordBot manages Discord slash command interactions for the supervisor.
type DiscordBot struct {
	session    DiscordSession
	guildID    string
	logger     *zap.Logger
	dispatcher *CommandDispatcher
	hub        *Hub
	tracker    *SessionTracker

	mu            sync.Mutex
	commandIDs    []string
	running       bool
	removeHandler func()
}

// NewDiscordBot creates a DiscordBot with a real discordgo session.
func NewDiscordBot(token, guildID string, dispatcher *CommandDispatcher, hub *Hub, tracker *SessionTracker, logger *zap.Logger) (*DiscordBot, error) {
	if token == "" {
		return nil, fmt.Errorf("discord bot token is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	return &DiscordBot{
		session:    &realDiscordSession{s: dg},
		guildID:    guildID,
		logger:     logger,
		dispatcher: dispatcher,
		hub:        hub,
		tracker:    tracker,
	}, nil
}

// NewDiscordBotWithSession creates a DiscordBot with an injected session (for testing).
func NewDiscordBotWithSession(session DiscordSession, guildID string, dispatcher *CommandDispatcher, hub *Hub, tracker *SessionTracker, logger *zap.Logger) *DiscordBot {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DiscordBot{
		session:    session,
		guildID:    guildID,
		logger:     logger,
		dispatcher: dispatcher,
		hub:        hub,
		tracker:    tracker,
	}
}

// slashCommands returns the 9 slash command definitions.
func slashCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        "status",
			Description: "Show session status for a project",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "project",
					Description: "Project name",
					Required:    true,
				},
			},
		},
		{
			Name:        "nodes",
			Description: "List connected agent nodes",
		},
		{
			Name:        "logs",
			Description: "Fetch recent session logs",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "session_id",
					Description: "Session ID to fetch logs for",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "limit",
					Description: "Number of log lines (default 20)",
					Required:    false,
				},
			},
		},
		{
			Name:        "resume",
			Description: "Resume a project session with a message",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "project",
					Description: "Project name",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "message",
					Description: "Message to send",
					Required:    true,
				},
			},
		},
		{
			Name:        "inject",
			Description: "Inject a message into a session",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "session_id",
					Description: "Target session ID",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "message",
					Description: "Message to inject",
					Required:    true,
				},
			},
		},
		{
			Name:        "restart",
			Description: "Restart a session",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "session_id",
					Description: "Session ID to restart",
					Required:    true,
				},
			},
		},
		{
			Name:        "kill",
			Description: "Kill a session",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "session_id",
					Description: "Session ID to kill",
					Required:    true,
				},
			},
		},
		{
			Name:        "start",
			Description: "Start a new session for a project",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "project",
					Description: "Project name",
					Required:    true,
				},
			},
		},
		{
			Name:        "cost",
			Description: "Show cost summary",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "period",
					Description: "Time period: today, week, month (default today)",
					Required:    false,
				},
			},
		},
	}
}

// Start opens the Discord session, registers commands, and sets up the interaction handler.
func (b *DiscordBot) Start() error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("discord bot is already running")
	}
	b.mu.Unlock()

	b.removeHandler = b.session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		b.handleInteraction(i)
	})

	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}

	state := b.session.State()
	appID := ""
	if state != nil && state.User != nil {
		appID = state.User.ID
	}

	var registeredIDs []string
	for _, cmd := range slashCommands() {
		registered, err := b.session.ApplicationCommandCreate(appID, b.guildID, cmd)
		if err != nil {
			b.logger.Warn("failed to register slash command",
				zap.String("command", cmd.Name),
				zap.Error(err),
			)
			continue
		}
		registeredIDs = append(registeredIDs, registered.ID)
		b.logger.Info("registered slash command", zap.String("command", cmd.Name))
	}

	b.mu.Lock()
	b.commandIDs = registeredIDs
	b.running = true
	b.mu.Unlock()

	return nil
}

// Stop deregisters commands and closes the Discord session.
func (b *DiscordBot) Stop() error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	state := b.session.State()
	appID := ""
	if state != nil && state.User != nil {
		appID = state.User.ID
	}

	b.mu.Lock()
	ids := b.commandIDs
	b.commandIDs = nil
	b.mu.Unlock()

	for _, id := range ids {
		if err := b.session.ApplicationCommandDelete(appID, b.guildID, id); err != nil {
			b.logger.Warn("failed to delete slash command", zap.String("id", id), zap.Error(err))
		}
	}

	if b.removeHandler != nil {
		b.removeHandler()
	}

	if err := b.session.Close(); err != nil {
		return fmt.Errorf("close discord session: %w", err)
	}

	b.mu.Lock()
	b.running = false
	b.mu.Unlock()

	return nil
}

// handleInteraction routes incoming interactions to the appropriate command handler.
func (b *DiscordBot) handleInteraction(i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			panicErr := fmt.Errorf("panic: %v", r)
			b.logger.Error("panic in interaction handler",
				zap.Error(panicErr),
				zap.Any("panic", r),
				zap.String("command", i.ApplicationCommandData().Name),
			)
			_, _ = b.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
				Embeds: []*discordgo.MessageEmbed{errorEmbed("Internal Error", "An unexpected error occurred. Please try again.")},
			})
		}
	}()

	data := i.ApplicationCommandData()
	cmdName := data.Name

	if err := b.session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		b.logger.Error("failed to acknowledge interaction", zap.String("command", cmdName), zap.Error(err))
		return
	}

	opts := make(map[string]*discordgo.ApplicationCommandInteractionDataOption)
	for _, opt := range data.Options {
		opts[opt.Name] = opt
	}

	var embed *discordgo.MessageEmbed

	switch cmdName {
	case "status":
		embed = b.handleStatus(opts)
	case "nodes":
		embed = b.handleNodes()
	case "logs":
		embed = b.handleLogs(opts)
	case "resume":
		embed = b.handleResume(opts)
	case "inject":
		embed = b.handleInject(opts)
	case "restart":
		embed = b.handleRestart(opts)
	case "kill":
		embed = b.handleKill(opts)
	case "start":
		embed = b.handleStart(opts)
	case "cost":
		embed = b.handleCost(opts)
	default:
		embed = errorEmbed("Unknown Command", fmt.Sprintf("Command `/%s` is not recognized.", cmdName))
	}

	if _, err := b.session.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	}); err != nil {
		b.logger.Error("failed to send followup", zap.String("command", cmdName), zap.Error(err))
	}
}

// handleStatus dispatches a session_status command for the given project.
func (b *DiscordBot) handleStatus(opts map[string]*discordgo.ApplicationCommandInteractionDataOption) *discordgo.MessageEmbed {
	projectOpt, ok := opts["project"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `project`")
	}
	project := projectOpt.StringValue()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := b.dispatcher.DispatchCommand(ctx, Command{
		Type:   CommandTypeSessionStatus,
		Target: CommandTarget{Project: project},
	})
	if err != nil {
		return errorEmbed("Status Failed", "Could not retrieve status. Please try again later.")
	}

	return commandResultEmbed("Status: "+project, result)
}

// handleNodes queries the hub for connected agent count and the registry for node details.
func (b *DiscordBot) handleNodes() *discordgo.MessageEmbed {
	count := 0
	if b.hub != nil {
		count = b.hub.ClientCount()
	}

	var description string
	nodes := make([]NodeEntry, 0)
	if b.dispatcher != nil && b.dispatcher.registry != nil {
		nodes = b.dispatcher.registry.ListNodes()
	}
	if len(nodes) > 0 {
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].ID < nodes[j].ID
		})

		lines := []string{
			fmt.Sprintf("**Connected:** %d", count),
			fmt.Sprintf("**Known nodes:** %d", len(nodes)),
		}

		const maxNodeLines = 10
		for i, node := range nodes {
			if i >= maxNodeLines {
				lines = append(lines, fmt.Sprintf("...and %d more", len(nodes)-maxNodeLines))
				break
			}

			lastHeartbeat := "-"
			if !node.LastHeartbeat.IsZero() {
				lastHeartbeat = node.LastHeartbeat.UTC().Format("2006-01-02 15:04:05")
			}

			lines = append(lines, fmt.Sprintf("`%s` (%s) - %s - hb: %s",
				valueOrDash(node.ID),
				valueOrDash(node.Hostname),
				node.Status,
				lastHeartbeat,
			))
		}

		description = strings.Join(lines, "\n")
	} else if b.tracker != nil {
		sessions := b.tracker.GetAllSessions()
		nodeSet := make(map[string]bool)
		for _, s := range sessions {
			nodeSet[s.NodeID] = true
		}
		description = fmt.Sprintf("**Connected:** %d\n**Known nodes:** %d", count, len(nodeSet))
	} else {
		description = fmt.Sprintf("**Connected:** %d", count)
	}

	return &discordgo.MessageEmbed{
		Title:       "Agent Nodes",
		Description: description,
		Color:       colorInfo,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// handleLogs returns session info as a proxy for logs (direct query, not dispatched).
func (b *DiscordBot) handleLogs(opts map[string]*discordgo.ApplicationCommandInteractionDataOption) *discordgo.MessageEmbed {
	sessionOpt, ok := opts["session_id"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `session_id`")
	}
	sessionID := sessionOpt.StringValue()

	if b.tracker == nil {
		return errorEmbed("Logs Unavailable", "Session tracker is not available.")
	}

	session, err := b.tracker.GetSession(sessionID)
	if err != nil {
		return errorEmbed("Session Not Found", fmt.Sprintf("No session found with ID `%s`.", sessionID))
	}

	limit := 20
	if limitOpt, ok := opts["limit"]; ok {
		limit = int(limitOpt.IntValue())
		if limit <= 0 {
			limit = 20
		}
	}

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Logs: %s", sessionID),
		Description: fmt.Sprintf("Session for project **%s** on node `%s`\nStatus: **%s**\nRequested lines: %d", session.Project, session.NodeID, session.Status, limit),
		Color:       colorInfo,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Model", Value: valueOrDash(session.Model), Inline: true},
			{Name: "Task", Value: valueOrDash(session.CurrentTask), Inline: true},
			{Name: "Tokens", Value: fmt.Sprintf("%d", session.TokenUsage.Total), Inline: true},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// handleResume dispatches a prompt_session command targeted by project.
func (b *DiscordBot) handleResume(opts map[string]*discordgo.ApplicationCommandInteractionDataOption) *discordgo.MessageEmbed {
	projectOpt, ok := opts["project"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `project`")
	}
	messageOpt, ok := opts["message"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `message`")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := b.dispatcher.DispatchCommand(ctx, Command{
		Type:   CommandTypePromptSession,
		Target: CommandTarget{Project: projectOpt.StringValue()},
		Args:   map[string]interface{}{"message": messageOpt.StringValue()},
	})
	if err != nil {
		return errorEmbed("Resume Failed", "Could not resume session. Please try again later.")
	}

	return commandResultEmbed("Resume: "+projectOpt.StringValue(), result)
}

// handleInject dispatches a prompt_session command targeted by session_id.
func (b *DiscordBot) handleInject(opts map[string]*discordgo.ApplicationCommandInteractionDataOption) *discordgo.MessageEmbed {
	sessionOpt, ok := opts["session_id"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `session_id`")
	}
	messageOpt, ok := opts["message"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `message`")
	}

	nodeID := ""
	if b.tracker != nil {
		if session, err := b.tracker.GetSession(sessionOpt.StringValue()); err == nil {
			nodeID = session.NodeID
		}
	}

	target := CommandTarget{}
	if nodeID != "" {
		target.NodeID = nodeID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := b.dispatcher.DispatchCommand(ctx, Command{
		Type:   CommandTypePromptSession,
		Target: target,
		Args:   map[string]interface{}{"message": messageOpt.StringValue(), "session_id": sessionOpt.StringValue()},
	})
	if err != nil {
		return errorEmbed("Inject Failed", "Could not inject message. Please try again later.")
	}

	return commandResultEmbed("Inject: "+sessionOpt.StringValue(), result)
}

// handleRestart dispatches a restart_session command.
func (b *DiscordBot) handleRestart(opts map[string]*discordgo.ApplicationCommandInteractionDataOption) *discordgo.MessageEmbed {
	sessionOpt, ok := opts["session_id"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `session_id`")
	}

	nodeID := ""
	if b.tracker != nil {
		if session, err := b.tracker.GetSession(sessionOpt.StringValue()); err == nil {
			nodeID = session.NodeID
		}
	}

	target := CommandTarget{}
	if nodeID != "" {
		target.NodeID = nodeID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := b.dispatcher.DispatchCommand(ctx, Command{
		Type:   CommandTypeRestartSession,
		Target: target,
		Args:   map[string]interface{}{"session_id": sessionOpt.StringValue()},
	})
	if err != nil {
		return errorEmbed("Restart Failed", "Could not restart session. Please try again later.")
	}

	return commandResultEmbed("Restart: "+sessionOpt.StringValue(), result)
}

// handleKill dispatches a kill_session command.
func (b *DiscordBot) handleKill(opts map[string]*discordgo.ApplicationCommandInteractionDataOption) *discordgo.MessageEmbed {
	sessionOpt, ok := opts["session_id"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `session_id`")
	}

	nodeID := ""
	if b.tracker != nil {
		if session, err := b.tracker.GetSession(sessionOpt.StringValue()); err == nil {
			nodeID = session.NodeID
		}
	}

	target := CommandTarget{}
	if nodeID != "" {
		target.NodeID = nodeID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := b.dispatcher.DispatchCommand(ctx, Command{
		Type:   CommandTypeKillSession,
		Target: target,
		Args:   map[string]interface{}{"session_id": sessionOpt.StringValue()},
	})
	if err != nil {
		return errorEmbed("Kill Failed", "Could not kill session. Please try again later.")
	}

	return commandResultEmbed("Kill: "+sessionOpt.StringValue(), result)
}

// handleStart dispatches a create_session command.
func (b *DiscordBot) handleStart(opts map[string]*discordgo.ApplicationCommandInteractionDataOption) *discordgo.MessageEmbed {
	projectOpt, ok := opts["project"]
	if !ok {
		return validationErrorEmbed("Missing required argument: `project`")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := b.dispatcher.DispatchCommand(ctx, Command{
		Type:   CommandTypeCreateSession,
		Target: CommandTarget{Project: projectOpt.StringValue()},
	})
	if err != nil {
		return errorEmbed("Start Failed", "Could not start session. Please try again later.")
	}

	return commandResultEmbed("Start: "+projectOpt.StringValue(), result)
}

// handleCost queries session cost data.
func (b *DiscordBot) handleCost(opts map[string]*discordgo.ApplicationCommandInteractionDataOption) *discordgo.MessageEmbed {
	period := "today"
	if periodOpt, ok := opts["period"]; ok {
		period = strings.ToLower(periodOpt.StringValue())
	}
	if period != "today" && period != "week" && period != "month" {
		period = "today"
	}

	if b.tracker == nil {
		return errorEmbed("Cost Unavailable", "Session tracker is not available.")
	}

	sessions := b.tracker.GetAllSessions()
	totalCost := 0.0
	totalTokens := 0
	for _, s := range sessions {
		totalCost += s.SessionCost
		totalTokens += s.TokenUsage.Total
	}

	return &discordgo.MessageEmbed{
		Title:       "Cost Summary",
		Description: fmt.Sprintf("Period: **%s**", period),
		Color:       colorInfo,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Total Cost", Value: fmt.Sprintf("$%.2f", totalCost), Inline: true},
			{Name: "Total Tokens", Value: fmt.Sprintf("%d", totalTokens), Inline: true},
			{Name: "Sessions", Value: fmt.Sprintf("%d", len(sessions)), Inline: true},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// commandResultEmbed converts a CommandResult into a Discord embed.
func commandResultEmbed(title string, result *CommandResult) *discordgo.MessageEmbed {
	if result == nil {
		return errorEmbed(title, "No result received.")
	}

	color := colorSuccess
	status := string(result.Status)

	switch result.Status {
	case CommandStatusFailure:
		color = colorFailure
	case CommandStatusTimeout:
		color = colorTimeout
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: "Status", Value: status, Inline: true},
		{Name: "Command ID", Value: result.CommandID, Inline: true},
	}

	description := ""
	if result.Output != "" {
		description = result.Output
	}
	if result.Error != "" {
		description = sanitizeError(result.Error)
	}

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		Color:       color,
		Fields:      fields,
		Timestamp:   result.Timestamp.Format(time.RFC3339),
	}
}

// errorEmbed creates a red error embed with a safe message.
func errorEmbed(title, description string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		Color:       colorError,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// validationErrorEmbed creates a red embed for validation/argument errors.
func validationErrorEmbed(description string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "Validation Error",
		Description: description,
		Color:       colorError,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// sanitizeError strips internal details from error messages.
func sanitizeError(err string) string {
	if strings.Contains(err, "node") && strings.Contains(err, "not connected") {
		return "Target node is not connected."
	}
	if strings.Contains(err, "not found") {
		return "The requested resource was not found."
	}
	if strings.Contains(err, "offline") {
		return "Target node is offline."
	}
	if strings.Contains(err, "timed out") {
		return "The command timed out. Please try again."
	}
	if strings.Contains(err, "saturated") {
		return "Target node is busy. Please try again later."
	}
	return "An error occurred while processing the command."
}

// valueOrDash returns the value or "-" if empty.
func valueOrDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
