package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Bldg-7/hal-o-swarm/internal/agent"
	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

type OAuthResult struct {
	Status         string              `json:"status"`
	ChallengeURL   string              `json:"challenge_url,omitempty"`
	UserCode       string              `json:"user_code,omitempty"`
	Reason         string              `json:"reason,omitempty"`
	ManualGuidance *ManualAuthGuidance `json:"manual_guidance,omitempty"`
}

type OAuthOrchestrator struct {
	dispatcher *CommandDispatcher
	logger     *zap.Logger
}

func NewOAuthOrchestrator(dispatcher *CommandDispatcher, logger *zap.Logger) *OAuthOrchestrator {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &OAuthOrchestrator{
		dispatcher: dispatcher,
		logger:     logger,
	}
}

func (o *OAuthOrchestrator) TriggerOAuth(ctx context.Context, nodeID string, tool shared.ToolIdentifier) (*OAuthResult, error) {
	if o.dispatcher == nil {
		return nil, fmt.Errorf("command dispatcher unavailable")
	}

	capability := agent.GetToolCapability(agent.ToolID(tool))
	if capability == nil {
		guidance := GenerateManualGuidance(tool, nodeID)
		return &OAuthResult{
			Status:         "manual_required",
			Reason:         fmt.Sprintf("tool %q is not supported for remote oauth", tool),
			ManualGuidance: &guidance,
		}, nil
	}

	if !capability.RemoteOAuth || !supportsOAuthFlow(capability.OAuthFlows, "device_code") {
		reason := capability.ManualLoginHint
		if strings.TrimSpace(reason) == "" {
			reason = fmt.Sprintf("tool %q requires manual authentication", tool)
		}
		guidance := GenerateManualGuidance(tool, nodeID)
		return &OAuthResult{
			Status:         "manual_required",
			Reason:         reason,
			ManualGuidance: &guidance,
		}, nil
	}

	command := Command{
		Type:   CommandTypeOAuthTrigger,
		Target: CommandTarget{NodeID: nodeID},
		Args: map[string]interface{}{
			"tool": string(tool),
		},
	}

	commandResult, err := o.dispatcher.DispatchCommand(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("dispatch oauth trigger command: %w", err)
	}

	if commandResult == nil {
		return nil, fmt.Errorf("empty oauth trigger command result")
	}

	if parsed, ok := parseOAuthCommandOutput(commandResult.Output); ok {
		if parsed.Status == "" {
			parsed.Status = string(commandResult.Status)
		}
		if parsed.Status == "failure" && parsed.Reason == "" {
			parsed.Reason = commandResult.Error
		}
		return parsed, nil
	}

	if commandResult.Status != CommandStatusSuccess {
		reason := commandResult.Error
		if reason == "" {
			reason = commandResult.Output
		}
		return &OAuthResult{Status: "failure", Reason: reason}, nil
	}

	if commandResult.Output != "" {
		return &OAuthResult{Status: "success", Reason: commandResult.Output}, nil
	}

	return &OAuthResult{Status: "success"}, nil
}

func parseOAuthCommandOutput(output string) (*OAuthResult, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, false
	}

	var result OAuthResult
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return nil, false
	}

	return &result, true
}

func supportsOAuthFlow(flows []string, flow string) bool {
	for _, candidate := range flows {
		if strings.EqualFold(strings.TrimSpace(candidate), flow) {
			return true
		}
	}
	return false
}
