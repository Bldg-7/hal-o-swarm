package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

type OAuthTriggerResult struct {
	Status       string `json:"status"`
	ChallengeURL string `json:"challenge_url,omitempty"`
	UserCode     string `json:"user_code,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type OAuthTriggerExecutor struct {
	runner AuthRunner
	logger *zap.Logger
}

func NewOAuthTriggerExecutor(runner AuthRunner, logger *zap.Logger) *OAuthTriggerExecutor {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &OAuthTriggerExecutor{runner: runner, logger: logger}
}

func RegisterOAuthTriggerHandler(client *WSClient, executor *OAuthTriggerExecutor) error {
	if client == nil {
		return fmt.Errorf("ws client is required")
	}
	if executor == nil {
		return fmt.Errorf("oauth trigger executor is required")
	}

	client.RegisterCommandHandler("oauth_trigger", HandleOAuthTrigger(executor))
	return nil
}

func HandleOAuthTrigger(executor *OAuthTriggerExecutor) CommandHandler {
	return func(ctx context.Context, envelope *shared.Envelope) error {
		result, err := ExecuteOAuthTriggerCommand(ctx, envelope, executor)
		if err != nil {
			return err
		}

		executor.logger.Info("oauth trigger command processed",
			zap.String("request_id", envelope.RequestID),
			zap.String("status", result.Status),
			zap.String("challenge_url", result.ChallengeURL),
			zap.String("user_code", result.UserCode),
			zap.String("reason", result.Reason),
		)

		return nil
	}
}

func ExecuteOAuthTriggerCommand(ctx context.Context, envelope *shared.Envelope, executor *OAuthTriggerExecutor) (OAuthTriggerResult, error) {
	if executor == nil {
		return OAuthTriggerResult{}, fmt.Errorf("oauth trigger executor is required")
	}
	if envelope == nil {
		return OAuthTriggerResult{}, fmt.Errorf("command envelope is required")
	}

	var command struct {
		Type string `json:"type"`
		Args struct {
			Tool shared.ToolIdentifier `json:"tool"`
		} `json:"args"`
	}

	if err := json.Unmarshal(envelope.Payload, &command); err != nil {
		return OAuthTriggerResult{}, fmt.Errorf("unmarshal oauth_trigger command: %w", err)
	}
	if command.Type != "oauth_trigger" {
		return OAuthTriggerResult{}, fmt.Errorf("unexpected command type %q", command.Type)
	}

	return executor.Trigger(ctx, command.Args.Tool), nil
}

func (e *OAuthTriggerExecutor) Trigger(ctx context.Context, tool shared.ToolIdentifier) OAuthTriggerResult {
	capability := GetToolCapability(ToolID(tool))
	if capability == nil {
		return OAuthTriggerResult{Status: "manual_required", Reason: fmt.Sprintf("tool %q is not supported", tool)}
	}
	if !capability.RemoteOAuth || !oauthFlowSupported(capability.OAuthFlows, "device_code") {
		reason := capability.ManualLoginHint
		if strings.TrimSpace(reason) == "" {
			reason = fmt.Sprintf("tool %q requires manual authentication", tool)
		}
		return OAuthTriggerResult{Status: "manual_required", Reason: reason}
	}

	if e.runner == nil {
		return stubOAuthChallenge(tool)
	}

	command := oauthDeviceCommand(tool)
	runResult := e.runner.RunAuthCheck(ctx, command)
	if runResult.TimedOut {
		return OAuthTriggerResult{Status: "failure", Reason: "oauth command timed out"}
	}
	if runResult.Err != nil && runResult.ExitCode != 0 {
		reason := strings.TrimSpace(runResult.Stderr)
		if reason == "" {
			reason = runResult.Err.Error()
		}
		return OAuthTriggerResult{Status: "failure", Reason: reason}
	}

	if challengeURL, userCode := parseOAuthChallenge(runResult.Stdout + "\n" + runResult.Stderr); challengeURL != "" && userCode != "" {
		return OAuthTriggerResult{
			Status:       "challenge",
			ChallengeURL: challengeURL,
			UserCode:     userCode,
		}
	}

	if runResult.ExitCode == 0 {
		return OAuthTriggerResult{Status: "success"}
	}

	return OAuthTriggerResult{Status: "failure", Reason: "oauth command failed"}
}

func oauthDeviceCommand(tool shared.ToolIdentifier) []string {
	switch tool {
	case shared.ToolIdentifierCodex:
		return []string{"codex", "login", "--device-auth"}
	case shared.ToolIdentifierOpenCode:
		return []string{"opencode", "auth", "login", "--device-code"}
	default:
		return []string{}
	}
}

func stubOAuthChallenge(tool shared.ToolIdentifier) OAuthTriggerResult {
	switch tool {
	case shared.ToolIdentifierCodex:
		return OAuthTriggerResult{
			Status:       "challenge",
			ChallengeURL: "https://auth.openai.com/device",
			UserCode:     "CODEX-DEVICE-CODE",
		}
	case shared.ToolIdentifierOpenCode:
		return OAuthTriggerResult{
			Status:       "challenge",
			ChallengeURL: "https://github.com/login/device",
			UserCode:     "OPENCODE-DEVICE-CODE",
		}
	default:
		return OAuthTriggerResult{Status: "manual_required", Reason: fmt.Sprintf("tool %q is not supported", tool)}
	}
}

func oauthFlowSupported(flows []string, flow string) bool {
	for _, candidate := range flows {
		if strings.EqualFold(strings.TrimSpace(candidate), flow) {
			return true
		}
	}
	return false
}

var (
	deviceURLRegex  = regexp.MustCompile(`https?://[^\s]+`)
	userCodeRegex   = regexp.MustCompile(`(?i)(?:user\s*code|code)\s*[:=]\s*([A-Z0-9-]{4,})`)
	userCodeRegexV2 = regexp.MustCompile(`(?i)enter\s+code\s+([A-Z0-9-]{4,})`)
)

func parseOAuthChallenge(output string) (string, string) {
	url := ""
	if matches := deviceURLRegex.FindStringSubmatch(output); len(matches) > 0 {
		url = matches[0]
	}

	code := ""
	if matches := userCodeRegex.FindStringSubmatch(output); len(matches) > 1 {
		code = matches[1]
	} else if matches := userCodeRegexV2.FindStringSubmatch(output); len(matches) > 1 {
		code = matches[1]
	}

	return strings.TrimSpace(url), strings.TrimSpace(code)
}
