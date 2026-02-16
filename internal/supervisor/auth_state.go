package supervisor

import (
	"encoding/json"
	"fmt"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
)

func (r *NodeRegistry) HandleAuthStateMessage(nodeID string, payload []byte) error {
	if nodeID == "" {
		return fmt.Errorf("auth_state node id is required")
	}

	var reports []shared.AuthStateReport
	if err := json.Unmarshal(payload, &reports); err != nil {
		return fmt.Errorf("unmarshal auth_state payload: %w", err)
	}
	if len(reports) == 0 {
		return fmt.Errorf("auth_state reports must not be empty")
	}

	states := make(map[string]NodeAuthState, len(reports))
	for _, report := range reports {
		tool := string(report.Tool)
		states[tool] = NodeAuthState{
			Tool:      tool,
			Status:    string(report.Status),
			Reason:    report.Reason,
			CheckedAt: report.CheckedAt,
		}
	}

	return r.UpdateAuthState(nodeID, states)
}
