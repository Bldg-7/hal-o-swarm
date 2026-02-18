package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hal-o-swarm/hal-o-swarm/internal/halctl"
)

var (
	supervisorURL = flag.String("supervisor-url", "http://localhost:8421", "Supervisor API URL")
	authToken     = flag.String("auth-token", "", "Authentication token (or set HALCTL_AUTH_TOKEN env var)")
	format        = flag.String("format", "table", "Output format: table or json")
)

func main() {
	flag.Parse()

	if *authToken == "" {
		*authToken = os.Getenv("HALCTL_AUTH_TOKEN")
	}

	if *authToken == "" {
		fmt.Fprintf(os.Stderr, "Error: auth token required (--auth-token or HALCTL_AUTH_TOKEN env var)\n")
		os.Exit(1)
	}

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	client := halctl.NewHTTPClient(*supervisorURL, *authToken)

	switch args[0] {
	case "sessions":
		handleSessions(client, args[1:])
	case "nodes":
		handleNodes(client, args[1:])
	case "cost":
		handleCost(client, args[1:])
	case "env":
		handleEnv(client, args[1:])
	case "auth":
		handleAuth(client, args[1:])
	case "agentmd":
		handleAgentMd(client, args[1:])
	case "init":
		handleInit(args[1:])
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n", args[0])
		os.Exit(1)
	}
}

func handleSessions(client *halctl.HTTPClient, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: sessions command requires subcommand (list, get, logs)\n")
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		sessions, err := halctl.ListSessions(client, "", "", "", 100)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(sessions)
		} else {
			printSessionsTable(sessions)
		}

	case "get":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: sessions get requires session id\n")
			os.Exit(1)
		}
		session, err := halctl.GetSession(client, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(session)
		} else {
			printSessionTable(session)
		}

	case "logs":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: sessions logs requires session id\n")
			os.Exit(1)
		}
		events, err := halctl.GetSessionLogs(client, args[1], 100)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(events)
		} else {
			printEventsTable(events)
		}

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown sessions subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func handleNodes(client *halctl.HTTPClient, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: nodes command requires subcommand (list, get)\n")
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		nodes, err := halctl.ListNodes(client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(nodes)
		} else {
			printNodesTable(nodes)
		}

	case "get":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: nodes get requires node id\n")
			os.Exit(1)
		}
		node, err := halctl.GetNode(client, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(node)
		} else {
			printNodeTable(node)
		}

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown nodes subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func handleCost(client *halctl.HTTPClient, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: cost command requires subcommand (today, week, month)\n")
		os.Exit(1)
	}

	var cost *halctl.CostSummary
	var err error

	switch args[0] {
	case "today":
		cost, err = halctl.GetCostToday(client)
	case "week":
		cost, err = halctl.GetCostWeek(client)
	case "month":
		cost, err = halctl.GetCostMonth(client)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown cost subcommand %q\n", args[0])
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *format == "json" {
		printJSON(cost)
	} else {
		printCostTable(cost)
	}
}

func handleEnv(client *halctl.HTTPClient, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: env command requires subcommand (status, check, provision)\n")
		os.Exit(1)
	}

	switch args[0] {
	case "status":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: env status requires project name\n")
			os.Exit(1)
		}
		result, err := halctl.GetEnvStatus(client, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(result)
		} else {
			printEnvCheckTable(result)
		}

	case "check":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: env check requires project name\n")
			os.Exit(1)
		}
		result, err := halctl.CheckEnv(client, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(result)
		} else {
			printEnvCheckTable(result)
		}

	case "provision":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: env provision requires project name\n")
			os.Exit(1)
		}
		result, err := halctl.ProvisionEnv(client, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(result)
		} else {
			printEnvProvisionTable(result)
		}

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown env subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func handleAuth(client *halctl.HTTPClient, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: auth command requires subcommand (status, drift)\n")
		os.Exit(1)
	}

	switch args[0] {
	case "status":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: auth status requires node id\n")
			os.Exit(1)
		}
		status, err := halctl.GetAuthStatus(client, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(status)
		} else {
			fmt.Print(halctl.FormatAuthStatusTable(status))
		}

	case "drift":
		drifted, err := halctl.GetDrift(client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(drifted)
		} else {
			fmt.Print(halctl.FormatDriftTable(drifted))
		}

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown auth subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func handleAgentMd(client *halctl.HTTPClient, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: agentmd command requires subcommand (diff, sync)\n")
		os.Exit(1)
	}

	switch args[0] {
	case "diff":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: agentmd diff requires project name\n")
			os.Exit(1)
		}
		diff, err := halctl.GetAgentMdDiff(client, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(diff)
		} else {
			printAgentMdDiffTable(diff)
		}

	case "sync":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: agentmd sync requires project name\n")
			os.Exit(1)
		}
		result, err := halctl.SyncAgentMd(client, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if *format == "json" {
			printJSON(result)
		} else {
			printAgentMdSyncTable(result)
		}

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown agentmd subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func printJSON(data interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}

func printSessionsTable(sessions []halctl.SessionJSON) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNODE_ID\tPROJECT\tSTATUS\tTOKENS\tCOST\tSTARTED_AT")
	for _, s := range sessions {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%.4f\t%s\n",
			s.ID, s.NodeID, s.Project, s.Status, s.Tokens, s.Cost, s.StartedAt.Format("2006-01-02 15:04:05"))
	}
	w.Flush()
}

func printSessionTable(session *halctl.SessionJSON) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE")
	fmt.Fprintf(w, "ID\t%s\n", session.ID)
	fmt.Fprintf(w, "NODE_ID\t%s\n", session.NodeID)
	fmt.Fprintf(w, "PROJECT\t%s\n", session.Project)
	fmt.Fprintf(w, "STATUS\t%s\n", session.Status)
	fmt.Fprintf(w, "TOKENS\t%d\n", session.Tokens)
	fmt.Fprintf(w, "COST\t%.4f\n", session.Cost)
	fmt.Fprintf(w, "STARTED_AT\t%s\n", session.StartedAt.Format("2006-01-02 15:04:05"))
	w.Flush()
}

func printEventsTable(events []halctl.EventJSON) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSESSION_ID\tTYPE\tTIMESTAMP")
	for _, e := range events {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			e.ID, e.SessionID, e.Type, e.Timestamp.Format("2006-01-02 15:04:05"))
	}
	w.Flush()
}

func printNodesTable(nodes []halctl.NodeJSON) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tHOSTNAME\tSTATUS\tLAST_HEARTBEAT\tCONNECTED_AT")
	for _, n := range nodes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			n.ID, n.Hostname, n.Status,
			n.LastHeartbeat.Format("2006-01-02 15:04:05"),
			n.ConnectedAt.Format("2006-01-02 15:04:05"))
	}
	w.Flush()
}

func printNodeTable(node *halctl.NodeJSON) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE")
	fmt.Fprintf(w, "ID\t%s\n", node.ID)
	fmt.Fprintf(w, "HOSTNAME\t%s\n", node.Hostname)
	fmt.Fprintf(w, "STATUS\t%s\n", node.Status)
	fmt.Fprintf(w, "LAST_HEARTBEAT\t%s\n", node.LastHeartbeat.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "CONNECTED_AT\t%s\n", node.ConnectedAt.Format("2006-01-02 15:04:05"))
	w.Flush()
}

func printCostTable(cost *halctl.CostSummary) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE")
	fmt.Fprintf(w, "PERIOD\t%s\n", cost.Period)
	fmt.Fprintf(w, "TOTAL_COST\t%.4f\n", cost.TotalCost)
	fmt.Fprintf(w, "SESSION_COUNT\t%d\n", cost.SessionCount)
	fmt.Fprintf(w, "TOKENS_USED\t%d\n", cost.TokensUsed)
	w.Flush()
}

func printEnvCheckTable(result *halctl.EnvCheckResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE")
	fmt.Fprintf(w, "PROJECT\t%s\n", result.Project)
	fmt.Fprintf(w, "STATUS\t%s\n", result.Status)
	if len(result.Issues) > 0 {
		fmt.Fprintf(w, "ISSUES\t%v\n", result.Issues)
	}
	w.Flush()
}

func printEnvProvisionTable(result *halctl.EnvProvisionResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE")
	fmt.Fprintf(w, "PROJECT\t%s\n", result.Project)
	fmt.Fprintf(w, "STATUS\t%s\n", result.Status)
	if len(result.Changes) > 0 {
		fmt.Fprintf(w, "CHANGES\t%v\n", result.Changes)
	}
	w.Flush()
}

func printAgentMdDiffTable(diff *halctl.AgentMdDiff) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE")
	fmt.Fprintf(w, "PROJECT\t%s\n", diff.Project)
	fmt.Fprintf(w, "DIFF\t%s\n", diff.Diff)
	w.Flush()
}

func printAgentMdSyncTable(result *halctl.AgentMdSyncResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tVALUE")
	fmt.Fprintf(w, "PROJECT\t%s\n", result.Project)
	fmt.Fprintf(w, "STATUS\t%s\n", result.Status)
	fmt.Fprintf(w, "MESSAGE\t%s\n", result.Message)
	w.Flush()
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `halctl - HAL-O-SWARM CLI

Usage:
  halctl [global-flags] <command> [subcommand] [args]

Global Flags:
  -supervisor-url string
        Supervisor API URL (default "http://localhost:8421")
  -auth-token string
        Authentication token (or set HALCTL_AUTH_TOKEN env var)
  -format string
        Output format: table or json (default "table")

Commands:
  sessions list                    List all sessions
  sessions get <id>                Get session details
  sessions logs <id>               Get session logs/events
  
  nodes list                       List all nodes
  nodes get <id>                   Get node details
  
  cost today                       Get today's cost
  cost week                        Get week's cost
  cost month                       Get month's cost
  
  env status <project>             Get environment status
  env check <project>              Check environment
  env provision <project>          Provision environment
  
  auth status <node-id>            Get auth status for a node
  auth drift                       List nodes with credential drift
  
  agentmd diff <project>           Show AGENT.md diff
  agentmd sync <project>           Sync AGENT.md

  init                             Interactive wizard for supervisor+agent config
  
  help                             Show this help message

Examples:
  halctl -auth-token mytoken sessions list
  halctl -format json nodes list
  halctl env status my-project
  halctl init
`)
}
