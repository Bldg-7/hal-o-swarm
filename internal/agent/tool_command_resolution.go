package agent

import (
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

var toolBinaryFallbacks = map[ToolID][]string{
	ToolOpencode:   {"/usr/local/bin/opencode", "/usr/bin/opencode", "/snap/bin/opencode"},
	ToolClaudeCode: {"/usr/local/bin/claude", "/usr/bin/claude", "/snap/bin/claude"},
	ToolCodex:      {"/usr/local/bin/codex", "/usr/bin/codex", "/snap/bin/codex"},
}

func resolveStatusCommand(toolID ToolID, configuredPath string, logger *zap.Logger) []string {
	cap := GetToolCapability(toolID)
	if cap == nil || len(cap.StatusCommand) == 0 {
		return nil
	}

	command := append([]string(nil), cap.StatusCommand...)
	binaryName := command[0]

	if configured := strings.TrimSpace(configuredPath); configured != "" {
		command[0] = configured
		if logger != nil {
			logger.Info("using configured tool path",
				zap.String("tool", string(toolID)),
				zap.String("binary", configured),
			)
		}
		return command
	}

	if _, err := exec.LookPath(binaryName); err == nil {
		return command
	}

	for _, candidate := range toolBinaryFallbacks[toolID] {
		if isExecutableFile(candidate) {
			command[0] = candidate
			if logger != nil {
				logger.Info("resolved tool binary from fallback path",
					zap.String("tool", string(toolID)),
					zap.String("binary", candidate),
				)
			}
			return command
		}
	}

	if logger != nil {
		logger.Warn("tool binary not found in PATH or fallback paths",
			zap.String("tool", string(toolID)),
			zap.String("binary", binaryName),
		)
	}

	return command
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	if info.IsDir() {
		return false
	}

	return info.Mode()&0o111 != 0
}
