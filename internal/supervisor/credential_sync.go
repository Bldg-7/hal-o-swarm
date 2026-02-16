package supervisor

import (
	"encoding/json"
	"fmt"
)

type CredentialSyncPayload struct {
	NodeID            string `json:"node_id"`
	CredentialVersion int    `json:"credential_version"`
}

func (r *NodeRegistry) HandleCredentialSyncMessage(payload []byte, expectedVersion int64) error {
	var syncPayload CredentialSyncPayload
	if err := json.Unmarshal(payload, &syncPayload); err != nil {
		return fmt.Errorf("unmarshal credential_sync payload: %w", err)
	}
	if syncPayload.NodeID == "" {
		return fmt.Errorf("credential_sync.node_id is required")
	}

	return r.ReconcileCredentialVersion(syncPayload.NodeID, syncPayload.CredentialVersion, expectedVersion)
}
