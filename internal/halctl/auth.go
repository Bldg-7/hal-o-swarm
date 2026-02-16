package halctl

import (
	"fmt"
	"text/tabwriter"
)

// GetAuthStatus retrieves and returns auth status for a node
func GetAuthStatus(client *HTTPClient, nodeID string) (*NodeAuthStatus, error) {
	return GetNodeAuth(client, nodeID)
}

// GetDrift retrieves and returns nodes with credential drift
func GetDrift(client *HTTPClient) ([]DriftNode, error) {
	return GetAuthDrift(client)
}

// FormatAuthStatusTable formats auth status as a table string
func FormatAuthStatusTable(status *NodeAuthStatus) string {
	var output string

	output += fmt.Sprintf("Node: %s\n", status.NodeID)
	output += fmt.Sprintf("Credential Sync: %s\n", status.CredentialSync)
	output += fmt.Sprintf("Credential Version: %d\n\n", status.CredentialVersion)

	output += "Tool            Status              Reason\n"
	output += "----            ------              ------\n"

	if len(status.AuthStates) == 0 {
		output += "(no auth states reported)\n"
	} else {
		for tool, state := range status.AuthStates {
			reason := state.Reason
			if reason == "" {
				reason = "-"
			}
			output += fmt.Sprintf("%-15s %-19s %s\n", tool, state.Status, reason)
		}
	}

	return output
}

// FormatDriftTable formats drift nodes as a table string
func FormatDriftTable(drifted []DriftNode) string {
	var output string

	output += "Node ID         Sync Status       Version\n"
	output += "-------         -----------       -------\n"

	if len(drifted) == 0 {
		output += "(no nodes with credential drift)\n"
	} else {
		for _, node := range drifted {
			output += fmt.Sprintf("%-15s %-17s %d\n", node.NodeID, node.CredentialSync, node.CredentialVersion)
		}
	}

	output += fmt.Sprintf("\nTotal: %d nodes with credential drift\n", len(drifted))

	return output
}

// FormatAuthStatusTableWriter writes auth status to a tabwriter
func FormatAuthStatusTableWriter(w *tabwriter.Writer, status *NodeAuthStatus) {
	fmt.Fprintf(w, "Node\t%s\n", status.NodeID)
	fmt.Fprintf(w, "Credential Sync\t%s\n", status.CredentialSync)
	fmt.Fprintf(w, "Credential Version\t%d\n", status.CredentialVersion)

	if len(status.AuthStates) > 0 {
		fmt.Fprintf(w, "\nTool\tStatus\tReason\n")
		for tool, state := range status.AuthStates {
			reason := state.Reason
			if reason == "" {
				reason = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", tool, state.Status, reason)
		}
	}
}
