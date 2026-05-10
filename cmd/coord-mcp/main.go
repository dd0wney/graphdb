// coord-mcp is an MCP server that exposes the graphdb coord daemon's
// claim/dependency primitives as Model Context Protocol tools, so any
// MCP-aware client (Cursor, Claude Desktop, VS Code's MCP plugin, etc.)
// can drive coord without the Claude-Code-specific bash skills.
//
// Wraps the existing REST + GraphQL surface — no new server-side
// primitives. The B-lite atomic uniqueness on :Claim.for_task
// (PR #91) is what makes coord_claim_task safe under concurrent
// invocation; without it, the MCP wrapper would inherit Taskmaster's
// "no atomic claim" weakness.
//
// Configuration (env vars):
//
//	GRAPHDB_COORD_URL    base URL of the coord daemon (default: http://localhost:8090)
//	GRAPHDB_COORD_TOKEN  X-API-Key value (required; from coord-bootstrap.sh)
//	COORD_PROJECT        project slug to scope task IDs to (default: auto-detect from git remote)
//
// Run: ./coord-mcp (reads/writes stdio). Wire into the MCP client of
// choice via its config (e.g. claude_desktop_config.json's mcpServers
// section).
//
// See docs/COMPARE_TASKMASTER_2026-05-10.md §7 for the rationale.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	client := newCoordClient(cfg)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "graphdb-coord",
		Version: "0.1.0",
	}, nil)

	registerTools(server, client)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server.Run: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

type config struct {
	BaseURL string
	APIKey  string
	Project string
}

func loadConfig() (*config, error) {
	c := &config{
		BaseURL: getenv("GRAPHDB_COORD_URL", "http://localhost:8090"),
		APIKey:  os.Getenv("GRAPHDB_COORD_TOKEN"),
		Project: os.Getenv("COORD_PROJECT"),
	}
	if c.APIKey == "" {
		// Fall back to ~/.graphdb-coord-key (the path coord-bootstrap.sh writes).
		home, _ := os.UserHomeDir()
		if data, err := os.ReadFile(home + "/.graphdb-coord-key"); err == nil {
			c.APIKey = strings.TrimSpace(string(data))
		}
	}
	if c.APIKey == "" {
		return nil, errors.New("GRAPHDB_COORD_TOKEN not set and ~/.graphdb-coord-key unreadable; run scripts/coord-bootstrap.sh")
	}
	if c.Project == "" {
		c.Project = detectProject()
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// detectProject mirrors what scripts/coord-seed.sh does: read
// `git remote get-url origin`, take basename, strip `.git`. Best-effort —
// returns "" if git fails or if not in a repo. Tools that need a project
// scope require the user to pass it via COORD_PROJECT in that case.
func detectProject() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	idx := strings.LastIndex(url, "/")
	if idx < 0 {
		return ""
	}
	slug := url[idx+1:]
	return strings.TrimSuffix(slug, ".git")
}

// ---------------------------------------------------------------------------
// Coord HTTP client
// ---------------------------------------------------------------------------

type coordClient struct {
	cfg  *config
	http *http.Client
}

func newCoordClient(cfg *config) *coordClient {
	return &coordClient{
		cfg:  cfg,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Node and Edge mirror what graphdb's REST endpoints emit. Properties
// values may be base64-encoded strings (H4.1 bug) — decodeStringProp
// handles the unwrap.
type Node struct {
	ID         int               `json:"id"`
	Labels     []string          `json:"labels"`
	Properties map[string]string `json:"properties"`
}

// Edge mirrors the GraphQL { edges { id type fromNodeId toNodeId } }
// shape. The ID/FromNodeID/ToNodeID fields land as GraphQL `ID` scalars,
// which serialize as JSON strings even though the underlying values are
// integers — keep them as string and convert via atoi when needed.
type Edge struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	FromNodeID string `json:"fromNodeId"`
	ToNodeID   string `json:"toNodeId"`
}

func (e Edge) FromID() int {
	n, _ := strconv.Atoi(e.FromNodeID)
	return n
}

func (e Edge) ToID() int {
	n, _ := strconv.Atoi(e.ToNodeID)
	return n
}

// decodeStringProp returns the property value as a string, unwrapping
// the H4.1 base64 encoding when present. If the value isn't valid
// base64 (e.g. an integer property), returns the raw string.
func decodeStringProp(raw string) string {
	if raw == "" {
		return ""
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
		// Heuristic: only treat as base64 if the decoded bytes look like
		// printable ASCII/UTF-8. Otherwise it was probably a numeric prop
		// that happens to base64-decode to garbage.
		if isPrintableUTF8(decoded) {
			return string(decoded)
		}
	}
	return raw
}

func isPrintableUTF8(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 && c != 0x09 && c != 0x0A && c != 0x0D {
			return false
		}
	}
	return true
}

func (c *coordClient) listNodes(ctx context.Context) ([]Node, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/nodes", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("listNodes: HTTP %d: %s", resp.StatusCode, body)
	}
	var nodes []Node
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// listEdges goes via GraphQL because REST GET /edges returns 405.
func (c *coordClient) listEdges(ctx context.Context) ([]Edge, error) {
	body := []byte(`{"query":"{ edges { id type fromNodeId toNodeId } }"}`)
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("listEdges: HTTP %d: %s", resp.StatusCode, b)
	}
	var wrapper struct {
		Data struct {
			Edges []Edge `json:"edges"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, err
	}
	return wrapper.Data.Edges, nil
}

func (c *coordClient) health(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// createNodeREST creates a node via REST POST /nodes. Does NOT route
// through B-lite uniqueness — use createNodeGraphQL for :Claim creation.
func (c *coordClient) createNodeREST(ctx context.Context, labels []string, props map[string]any) (int, error) {
	body, err := json.Marshal(map[string]any{"labels": labels, "properties": props})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/nodes", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("createNodeREST: HTTP %d: %s", resp.StatusCode, b)
	}
	var node Node
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		return 0, err
	}
	return node.ID, nil
}

// createNodeGraphQL creates a node via the GraphQL createNode mutation,
// which routes through the B-lite uniqueness check for :Claim. Returns
// either the new node ID or a *uniqueConflict on B-lite rejection.
func (c *coordClient) createNodeGraphQL(ctx context.Context, labels []string, props map[string]any) (int, error) {
	propsJSON, err := json.Marshal(props)
	if err != nil {
		return 0, err
	}
	labelsJSON, _ := json.Marshal(labels)
	query := fmt.Sprintf(
		"mutation { createNode(labels: %s, properties: %s) { id } }",
		string(labelsJSON), strconv.Quote(string(propsJSON)),
	)
	body, _ := json.Marshal(map[string]any{"query": query})
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var wrapper struct {
		Data struct {
			CreateNode *struct {
				ID string `json:"id"`
			} `json:"createNode"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return 0, err
	}
	if len(wrapper.Errors) > 0 {
		msg := wrapper.Errors[0].Message
		if strings.Contains(msg, "unique constraint") {
			conflictID := 0
			if m := regexp.MustCompile(`node (\d+)`).FindStringSubmatch(msg); len(m) > 1 {
				conflictID, _ = strconv.Atoi(m[1])
			}
			return 0, &uniqueConflict{Message: msg, ConflictingNodeID: conflictID}
		}
		return 0, errors.New(msg)
	}
	if wrapper.Data.CreateNode == nil {
		return 0, errors.New("createNode returned nil data")
	}
	id, err := strconv.Atoi(wrapper.Data.CreateNode.ID)
	return id, err
}

type uniqueConflict struct {
	Message           string
	ConflictingNodeID int
}

func (e *uniqueConflict) Error() string { return e.Message }

func (c *coordClient) createEdge(ctx context.Context, edgeType string, from, to int) (int, error) {
	body, _ := json.Marshal(map[string]any{
		"type":         edgeType,
		"from_node_id": from,
		"to_node_id":   to,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/edges", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("createEdge: HTTP %d: %s", resp.StatusCode, b)
	}
	// REST POST /edges returns numeric IDs (unlike the GraphQL edges
	// query which returns string ID scalars). Decode into a local
	// struct rather than reusing the Edge type.
	var rest struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rest); err != nil {
		return 0, err
	}
	return rest.ID, nil
}

func (c *coordClient) updateNode(ctx context.Context, id int, props map[string]any) error {
	propsJSON, _ := json.Marshal(props)
	query := fmt.Sprintf(
		`mutation { updateNode(id: "%d", properties: %s) { id } }`,
		id, strconv.Quote(string(propsJSON)),
	)
	body, _ := json.Marshal(map[string]any{"query": query})
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("updateNode: HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

func (c *coordClient) deleteNode(ctx context.Context, id int) error {
	query := fmt.Sprintf(`mutation { deleteNode(id: "%d") { success } }`, id)
	body, _ := json.Marshal(map[string]any{"query": query})
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deleteNode: HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tool registration
// ---------------------------------------------------------------------------

func registerTools(s *mcp.Server, c *coordClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "coord_health",
		Description: "Check the coord daemon's health endpoint. Returns status, uptime, edition, and feature flags. No arguments.",
	}, c.healthTool)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coord_next",
		Description: "Recommend the next Task to claim. Returns the highest-priority pending Task whose DEPENDS_ON dependencies are all satisfied. Tie-breaks on (most blocking, lowest node ID). Excludes Tasks already claimed. Read-only — does not mutate coord state.",
	}, c.nextTool)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coord_claim_task",
		Description: "Atomically claim a Task. Routes through B-lite GraphQL uniqueness so concurrent claims for the same task_id fail with a conflict error naming the holding Claim node. Creates :Agent (if missing), :Claim, and HOLDS+FOR edges; flips Task.status to in-progress.",
	}, c.claimTaskTool)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coord_release_claim",
		Description: "Release a Claim. Optionally creates a :PR node and CLOSED_BY edge if pr_number is provided. Flips Task.status to done before deleting the Claim (cascades HOLDS+FOR edges).",
	}, c.releaseClaimTool)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coord_clusters",
		Description: "Compute layered parallel-execution plan from the DEPENDS_ON graph. Layer 0 is fully unblocked now; layer N is unblocked once layer N-1 closes. Tasks in cycles surface separately. Read-only.",
	}, c.clustersTool)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coord_subtask",
		Description: "Add a child Task under an existing parent via :SUBTASK_OF edge. Subtask id is parent_id.<n> with auto-incremented n. Inherits parent's track; status=pending; nothing else inherited. Subtasks are first-class Tasks (claimable, etc.).",
	}, c.subtaskTool)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coord_status",
		Description: "Read a Task's current status, including any active Claim and unsatisfied DEPENDS_ON blockers. Read-only.",
	}, c.statusTool)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "coord_add_dependency",
		Description: "Add a :DEPENDS_ON edge from one Task to another. The from-Task is blocked until the to-Task is done or cancelled. Useful for seeding the planning-doc's sequencing graph into coord.",
	}, c.addDependencyTool)
}

// ---------------------------------------------------------------------------
// Tool handlers
// ---------------------------------------------------------------------------

type emptyInput struct{}

// --- coord_health ---

type healthOutput struct {
	Status   string   `json:"status"`
	Uptime   string   `json:"uptime"`
	Edition  string   `json:"edition,omitempty"`
	Features []string `json:"features,omitempty"`
}

func (c *coordClient) healthTool(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, healthOutput, error) {
	raw, err := c.health(ctx)
	if err != nil {
		return nil, healthOutput{}, err
	}
	out := healthOutput{
		Status:  asString(raw["status"]),
		Uptime:  asString(raw["uptime"]),
		Edition: asString(raw["edition"]),
	}
	if feats, ok := raw["features"].([]any); ok {
		for _, f := range feats {
			out.Features = append(out.Features, asString(f))
		}
	}
	text := fmt.Sprintf("coord daemon healthy: status=%s edition=%s uptime=%s", out.Status, out.Edition, out.Uptime)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
}

// --- coord_next ---

type taskRef struct {
	TaskID string `json:"task_id"`
	NodeID int    `json:"node_id"`
	Track  string `json:"track,omitempty"`
	Status string `json:"status,omitempty"`
	Blocks int    `json:"blocks,omitempty"`
}

type nextOutput struct {
	Recommended  *taskRef  `json:"recommended"`
	Alternatives []taskRef `json:"alternatives,omitempty"`
	Blocked      []taskRef `json:"blocked,omitempty"`
}

func (c *coordClient) nextTool(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, nextOutput, error) {
	state, err := c.loadState(ctx)
	if err != nil {
		return nil, nextOutput{}, err
	}
	candidates, blockedCs := state.candidatesByUnblocked()

	if len(candidates) == 0 {
		out := nextOutput{Blocked: blockedCs}
		text := "no unblocked pending Tasks."
		if len(blockedCs) > 0 {
			text += fmt.Sprintf(" %d blocked candidate(s).", len(blockedCs))
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Blocks != candidates[j].Blocks {
			return candidates[i].Blocks > candidates[j].Blocks
		}
		return candidates[i].NodeID < candidates[j].NodeID
	})

	out := nextOutput{
		Recommended:  &candidates[0],
		Alternatives: candidates[1:],
	}
	text := fmt.Sprintf("NEXT: %s (track=%s, blocks %d downstream)", out.Recommended.TaskID, out.Recommended.Track, out.Recommended.Blocks)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
}

// --- coord_claim_task ---

type claimInput struct {
	TaskID           string `json:"task_id" jsonschema:"the project-prefixed Task id, e.g. graphdb:F1.1-spike. Required."`
	ExpectedDuration string `json:"expected_duration,omitempty" jsonschema:"informational duration hint, e.g. 4h. Optional."`
	AgentID          string `json:"agent_id,omitempty" jsonschema:"override the auto-generated agent id. Optional."`
}

type claimOutput struct {
	Success          bool   `json:"success"`
	ClaimNodeID      int    `json:"claim_node_id,omitempty"`
	AgentNodeID      int    `json:"agent_node_id,omitempty"`
	TaskNodeID       int    `json:"task_node_id,omitempty"`
	Conflict         bool   `json:"conflict,omitempty"`
	ConflictingClaim int    `json:"conflicting_claim_node_id,omitempty"`
	ConflictMessage  string `json:"conflict_message,omitempty"`
}

func (c *coordClient) claimTaskTool(ctx context.Context, _ *mcp.CallToolRequest, in claimInput) (*mcp.CallToolResult, claimOutput, error) {
	if in.TaskID == "" {
		return nil, claimOutput{}, errors.New("task_id is required")
	}

	state, err := c.loadState(ctx)
	if err != nil {
		return nil, claimOutput{}, err
	}
	taskNodeID, ok := state.taskNodeIDByName(in.TaskID)
	if !ok {
		return nil, claimOutput{}, fmt.Errorf("no :Task node with id=%q — re-seed via scripts/coord-seed.sh", in.TaskID)
	}

	agentID := in.AgentID
	if agentID == "" {
		agentID = defaultAgentID()
	}
	agentNodeID, ok := state.agentNodeIDByName(agentID)
	if !ok {
		// Tolerated race: two concurrent claims by the same agent_id can
		// both miss the lookup and both create. B-lite is :Claim-specific,
		// so :Agent has no server-side uniqueness. Duplicates are
		// harmless (both reference the same agent identity); cleanup is
		// out of scope for this PR. Extending the resolver special-case
		// to :Agent.id would be a separate change.
		hostname, _ := os.Hostname()
		newID, err := c.createNodeREST(ctx, []string{"Agent"}, map[string]any{
			"id":         agentID,
			"host":       hostname,
			"started_at": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		})
		if err != nil {
			return nil, claimOutput{}, fmt.Errorf("create Agent: %w", err)
		}
		agentNodeID = newID
	}

	duration := in.ExpectedDuration
	if duration == "" {
		duration = "4h"
	}
	claimNodeID, err := c.createNodeGraphQL(ctx, []string{"Claim"}, map[string]any{
		"for_task":          in.TaskID,
		"started_at":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"expected_duration": duration,
	})
	if err != nil {
		var conflict *uniqueConflict
		if errors.As(err, &conflict) {
			out := claimOutput{
				Success:          false,
				Conflict:         true,
				ConflictingClaim: conflict.ConflictingNodeID,
				ConflictMessage:  conflict.Message,
				TaskNodeID:       taskNodeID,
				AgentNodeID:      agentNodeID,
			}
			text := fmt.Sprintf("CONFLICT: %s already claimed (Claim node %d holds it)", in.TaskID, conflict.ConflictingNodeID)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
		}
		return nil, claimOutput{}, err
	}

	if _, err := c.createEdge(ctx, "HOLDS", agentNodeID, claimNodeID); err != nil {
		return nil, claimOutput{}, fmt.Errorf("HOLDS edge: %w", err)
	}
	if _, err := c.createEdge(ctx, "FOR", claimNodeID, taskNodeID); err != nil {
		return nil, claimOutput{}, fmt.Errorf("FOR edge: %w", err)
	}
	if err := c.updateNode(ctx, taskNodeID, map[string]any{"status": "in-progress"}); err != nil {
		return nil, claimOutput{}, fmt.Errorf("flip Task.status: %w", err)
	}

	out := claimOutput{
		Success:     true,
		ClaimNodeID: claimNodeID,
		AgentNodeID: agentNodeID,
		TaskNodeID:  taskNodeID,
	}
	text := fmt.Sprintf("claimed %s — Claim node %d (Agent %d, Task %d)", in.TaskID, claimNodeID, agentNodeID, taskNodeID)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
}

// --- coord_release_claim ---

type releaseInput struct {
	ClaimNodeID int  `json:"claim_node_id" jsonschema:"the :Claim node id from coord_claim_task. Required."`
	TaskNodeID  int  `json:"task_node_id" jsonschema:"the :Task node id from coord_claim_task. Required."`
	PRNumber    int  `json:"pr_number,omitempty" jsonschema:"optional GitHub PR number. If set, creates a :PR node + CLOSED_BY edge."`
	Cancelled   bool `json:"cancelled,omitempty" jsonschema:"true if the Task was abandoned without completion. Sets status to cancelled instead of done; skips :PR + CLOSED_BY."`
}

type releaseOutput struct {
	Success   bool   `json:"success"`
	PRNodeID  int    `json:"pr_node_id,omitempty"`
	NewStatus string `json:"new_status"`
}

func (c *coordClient) releaseClaimTool(ctx context.Context, _ *mcp.CallToolRequest, in releaseInput) (*mcp.CallToolResult, releaseOutput, error) {
	if in.ClaimNodeID == 0 || in.TaskNodeID == 0 {
		return nil, releaseOutput{}, errors.New("claim_node_id and task_node_id are required")
	}

	prNodeID := 0
	newStatus := "done"
	if in.Cancelled {
		newStatus = "cancelled"
	} else if in.PRNumber != 0 {
		state, err := c.loadState(ctx)
		if err != nil {
			return nil, releaseOutput{}, err
		}
		prNodeID = state.prNodeIDByNumber(in.PRNumber)
		if prNodeID == 0 {
			id, err := c.createNodeREST(ctx, []string{"PR"}, map[string]any{"number": in.PRNumber})
			if err != nil {
				return nil, releaseOutput{}, fmt.Errorf("create PR node: %w", err)
			}
			prNodeID = id
		}
		if _, err := c.createEdge(ctx, "CLOSED_BY", in.TaskNodeID, prNodeID); err != nil {
			return nil, releaseOutput{}, fmt.Errorf("CLOSED_BY edge: %w", err)
		}
	}

	if err := c.updateNode(ctx, in.TaskNodeID, map[string]any{"status": newStatus}); err != nil {
		return nil, releaseOutput{}, fmt.Errorf("flip Task.status: %w", err)
	}
	if err := c.deleteNode(ctx, in.ClaimNodeID); err != nil {
		return nil, releaseOutput{}, fmt.Errorf("delete Claim: %w", err)
	}

	out := releaseOutput{Success: true, PRNodeID: prNodeID, NewStatus: newStatus}
	text := fmt.Sprintf("released Claim %d; Task %d → status=%s", in.ClaimNodeID, in.TaskNodeID, newStatus)
	if prNodeID != 0 {
		text += fmt.Sprintf(" (PR node %d)", prNodeID)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
}

// --- coord_clusters ---

type clustersOutput struct {
	Layers       [][]taskRef `json:"layers"`
	NotScheduled []taskRef   `json:"not_scheduled,omitempty"`
}

func (c *coordClient) clustersTool(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, clustersOutput, error) {
	state, err := c.loadState(ctx)
	if err != nil {
		return nil, clustersOutput{}, err
	}
	layers, notScheduled := state.parallelLayers()

	var pretty []string
	pretty = append(pretty, fmt.Sprintf("parallel-execution plan (%d layer(s)):", len(layers)))
	for i, layer := range layers {
		pretty = append(pretty, fmt.Sprintf("  layer %d (%d parallel):", i, len(layer)))
		for _, t := range layer {
			pretty = append(pretty, fmt.Sprintf("    %s (track=%s)", t.TaskID, t.Track))
		}
	}
	if len(notScheduled) > 0 {
		pretty = append(pretty, fmt.Sprintf("NOT scheduled (%d):", len(notScheduled)))
		for _, t := range notScheduled {
			pretty = append(pretty, fmt.Sprintf("    %s", t.TaskID))
		}
	}

	out := clustersOutput{Layers: layers, NotScheduled: notScheduled}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Join(pretty, "\n")}}}, out, nil
}

// --- coord_subtask ---

type subtaskInput struct {
	ParentTaskID string `json:"parent_task_id" jsonschema:"the project-prefixed parent Task id. Required."`
	Title        string `json:"title" jsonschema:"human-readable subtask title. Required."`
}

type subtaskOutput struct {
	SubtaskID     string `json:"subtask_id"`
	SubtaskNodeID int    `json:"subtask_node_id"`
	ParentNodeID  int    `json:"parent_node_id"`
	Track         string `json:"track"`
}

func (c *coordClient) subtaskTool(ctx context.Context, _ *mcp.CallToolRequest, in subtaskInput) (*mcp.CallToolResult, subtaskOutput, error) {
	if in.ParentTaskID == "" || in.Title == "" {
		return nil, subtaskOutput{}, errors.New("parent_task_id and title are required")
	}

	state, err := c.loadState(ctx)
	if err != nil {
		return nil, subtaskOutput{}, err
	}
	parent, ok := state.taskByName(in.ParentTaskID)
	if !ok {
		return nil, subtaskOutput{}, fmt.Errorf("no parent Task with id=%q", in.ParentTaskID)
	}

	maxN := 0
	for _, t := range state.tasks {
		if strings.HasPrefix(t.ID, in.ParentTaskID+".") {
			suffix := strings.TrimPrefix(t.ID, in.ParentTaskID+".")
			if n, err := strconv.Atoi(suffix); err == nil && n > maxN {
				maxN = n
			}
		}
	}
	subtaskID := fmt.Sprintf("%s.%d", in.ParentTaskID, maxN+1)

	newID, err := c.createNodeREST(ctx, []string{"Task"}, map[string]any{
		"id":         subtaskID,
		"track":      parent.Track,
		"status":     "pending",
		"title":      in.Title,
		"created_at": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
	if err != nil {
		return nil, subtaskOutput{}, fmt.Errorf("create subtask Task: %w", err)
	}
	if _, err := c.createEdge(ctx, "SUBTASK_OF", newID, parent.NodeID); err != nil {
		return nil, subtaskOutput{}, fmt.Errorf("SUBTASK_OF edge: %w", err)
	}
	// Best-effort IN_PROJECT (skip silently if no Project node exists for the prefix).
	projectSlug := strings.SplitN(in.ParentTaskID, ":", 2)[0]
	if pid := state.projectNodeIDBySlug(projectSlug); pid != 0 {
		_, _ = c.createEdge(ctx, "IN_PROJECT", newID, pid)
	}

	out := subtaskOutput{SubtaskID: subtaskID, SubtaskNodeID: newID, ParentNodeID: parent.NodeID, Track: parent.Track}
	text := fmt.Sprintf("subtask created: %s (node %d, track=%s)", subtaskID, newID, parent.Track)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
}

// --- coord_status ---

type statusInput struct {
	TaskID string `json:"task_id" jsonschema:"the project-prefixed Task id. Required."`
}

type statusOutput struct {
	TaskID         string    `json:"task_id"`
	NodeID         int       `json:"node_id"`
	Status         string    `json:"status"`
	Track          string    `json:"track"`
	ClaimNodeID    int       `json:"claim_node_id,omitempty"`
	HoldingAgentID string    `json:"holding_agent_id,omitempty"`
	Blockers       []taskRef `json:"blockers,omitempty"`
}

func (c *coordClient) statusTool(ctx context.Context, _ *mcp.CallToolRequest, in statusInput) (*mcp.CallToolResult, statusOutput, error) {
	if in.TaskID == "" {
		return nil, statusOutput{}, errors.New("task_id is required")
	}

	state, err := c.loadState(ctx)
	if err != nil {
		return nil, statusOutput{}, err
	}
	t, ok := state.taskByName(in.TaskID)
	if !ok {
		return nil, statusOutput{}, fmt.Errorf("no Task with id=%q", in.TaskID)
	}

	out := statusOutput{TaskID: t.ID, NodeID: t.NodeID, Status: t.Status, Track: t.Track}

	// Find an active Claim for this task, if any.
	for _, claim := range state.claims {
		if claim.ForTask != t.ID {
			continue
		}
		out.ClaimNodeID = claim.NodeID
		// Walk HOLDS edges backward from the Claim to find the holding Agent.
		for _, e := range state.edges {
			if e.Type == "HOLDS" && e.ToID() == claim.NodeID {
				if a, ok := state.agentByNodeID[e.FromID()]; ok {
					out.HoldingAgentID = a
				}
			}
		}
		break
	}

	// Unsatisfied DEPENDS_ON.
	for _, e := range state.edges {
		if e.Type != "DEPENDS_ON" || e.FromID() != t.NodeID {
			continue
		}
		dep, ok := state.taskByNodeID[e.ToID()]
		if !ok {
			continue
		}
		if dep.Status != "done" && dep.Status != "cancelled" {
			out.Blockers = append(out.Blockers, taskRef{
				TaskID: dep.ID,
				NodeID: dep.NodeID,
				Track:  dep.Track,
				Status: dep.Status,
			})
		}
	}

	parts := []string{
		fmt.Sprintf("status: %s", out.Status),
		fmt.Sprintf("track: %s", out.Track),
		fmt.Sprintf("node: %d", out.NodeID),
	}
	if out.ClaimNodeID != 0 {
		parts = append(parts, fmt.Sprintf("claimed by %s (Claim %d)", out.HoldingAgentID, out.ClaimNodeID))
	}
	if len(out.Blockers) > 0 {
		ids := make([]string, len(out.Blockers))
		for i, b := range out.Blockers {
			ids[i] = b.TaskID
		}
		parts = append(parts, fmt.Sprintf("blocked by: %s", strings.Join(ids, ", ")))
	}
	text := fmt.Sprintf("%s — %s", t.ID, strings.Join(parts, "; "))
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
}

// --- coord_add_dependency ---

type addDependencyInput struct {
	FromTaskID string `json:"from_task_id" jsonschema:"the dependent Task. Required."`
	ToTaskID   string `json:"to_task_id" jsonschema:"the Task that must close first. Required."`
}

type addDependencyOutput struct {
	EdgeID int `json:"edge_id"`
}

func (c *coordClient) addDependencyTool(ctx context.Context, _ *mcp.CallToolRequest, in addDependencyInput) (*mcp.CallToolResult, addDependencyOutput, error) {
	if in.FromTaskID == "" || in.ToTaskID == "" {
		return nil, addDependencyOutput{}, errors.New("from_task_id and to_task_id are required")
	}
	if in.FromTaskID == in.ToTaskID {
		return nil, addDependencyOutput{}, errors.New("a Task cannot depend on itself")
	}

	state, err := c.loadState(ctx)
	if err != nil {
		return nil, addDependencyOutput{}, err
	}
	from, ok := state.taskByName(in.FromTaskID)
	if !ok {
		return nil, addDependencyOutput{}, fmt.Errorf("no Task with id=%q (from)", in.FromTaskID)
	}
	to, ok := state.taskByName(in.ToTaskID)
	if !ok {
		return nil, addDependencyOutput{}, fmt.Errorf("no Task with id=%q (to)", in.ToTaskID)
	}

	edgeID, err := c.createEdge(ctx, "DEPENDS_ON", from.NodeID, to.NodeID)
	if err != nil {
		return nil, addDependencyOutput{}, err
	}

	out := addDependencyOutput{EdgeID: edgeID}
	text := fmt.Sprintf("DEPENDS_ON edge %d: %s → %s", edgeID, in.FromTaskID, in.ToTaskID)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
}

// ---------------------------------------------------------------------------
// State snapshot — one-shot read of the full coord graph for the
// read-side tools (coord_next, coord_clusters, coord_status). Cheap at
// coord scale; would need pagination at >1000 nodes.
// ---------------------------------------------------------------------------

type taskInfo struct {
	NodeID int
	ID     string
	Status string
	Track  string
}

type claimInfo struct {
	NodeID  int
	ForTask string
}

type stateSnapshot struct {
	tasks         map[int]taskInfo // node_id -> taskInfo
	tasksByName   map[string]int   // task id -> node_id
	claims        []claimInfo
	agentByNodeID map[int]string // agent node_id -> agent_id property
	projects      map[string]int // project slug -> node_id
	edges         []Edge
	taskByNodeID  map[int]taskInfo // alias for clarity in callers
}

func (c *coordClient) loadState(ctx context.Context) (*stateSnapshot, error) {
	nodes, err := c.listNodes(ctx)
	if err != nil {
		return nil, err
	}
	edges, err := c.listEdges(ctx)
	if err != nil {
		return nil, err
	}

	s := &stateSnapshot{
		tasks:         map[int]taskInfo{},
		tasksByName:   map[string]int{},
		agentByNodeID: map[int]string{},
		projects:      map[string]int{},
		edges:         edges,
	}
	for _, n := range nodes {
		switch {
		case hasLabel(n.Labels, "Task"):
			t := taskInfo{
				NodeID: n.ID,
				ID:     decodeStringProp(n.Properties["id"]),
				Status: decodeStringProp(n.Properties["status"]),
				Track:  decodeStringProp(n.Properties["track"]),
			}
			s.tasks[n.ID] = t
			s.tasksByName[t.ID] = n.ID
		case hasLabel(n.Labels, "Claim"):
			s.claims = append(s.claims, claimInfo{
				NodeID:  n.ID,
				ForTask: decodeStringProp(n.Properties["for_task"]),
			})
		case hasLabel(n.Labels, "Agent"):
			s.agentByNodeID[n.ID] = decodeStringProp(n.Properties["id"])
		case hasLabel(n.Labels, "Project"):
			s.projects[decodeStringProp(n.Properties["id"])] = n.ID
		}
	}
	s.taskByNodeID = s.tasks
	return s, nil
}

func (s *stateSnapshot) taskNodeIDByName(id string) (int, bool) {
	nid, ok := s.tasksByName[id]
	return nid, ok
}

func (s *stateSnapshot) taskByName(id string) (taskInfo, bool) {
	nid, ok := s.tasksByName[id]
	if !ok {
		return taskInfo{}, false
	}
	return s.tasks[nid], true
}

func (s *stateSnapshot) agentNodeIDByName(id string) (int, bool) {
	for nid, aid := range s.agentByNodeID {
		if aid == id {
			return nid, true
		}
	}
	return 0, false
}

func (s *stateSnapshot) projectNodeIDBySlug(slug string) int {
	return s.projects[slug]
}

func (s *stateSnapshot) prNodeIDByNumber(num int) int {
	// Re-scan nodes — we didn't index PRs in loadState. Cheap at coord scale.
	// Walk edges looking for CLOSED_BY targets, or fall through.
	for _, e := range s.edges {
		_ = e
	}
	return 0 // not indexed; caller creates a new PR node if 0
}

// claimedTaskIDs returns the set of for_task strings with active Claims.
func (s *stateSnapshot) claimedTaskIDs() map[string]bool {
	out := map[string]bool{}
	for _, c := range s.claims {
		out[c.ForTask] = true
	}
	return out
}

// dependsOn maps task_node_id → []dep_task_node_ids.
func (s *stateSnapshot) dependsOn() map[int][]int {
	out := map[int][]int{}
	for _, e := range s.edges {
		if e.Type == "DEPENDS_ON" {
			out[e.FromID()] = append(out[e.FromID()], e.ToID())
		}
	}
	return out
}

// blockCount maps task_node_id → number of tasks that depend on it.
func (s *stateSnapshot) blockCount() map[int]int {
	out := map[int]int{}
	for _, e := range s.edges {
		if e.Type == "DEPENDS_ON" {
			out[e.ToID()]++
		}
	}
	return out
}

// candidatesByUnblocked partitions pending+unclaimed Tasks into
// (unblocked, blocked).
func (s *stateSnapshot) candidatesByUnblocked() (unblocked, blocked []taskRef) {
	claimed := s.claimedTaskIDs()
	deps := s.dependsOn()
	blocks := s.blockCount()

	depsSatisfied := func(nid int) bool {
		for _, dep := range deps[nid] {
			d, ok := s.tasks[dep]
			if !ok {
				continue
			}
			if d.Status != "done" && d.Status != "cancelled" {
				return false
			}
		}
		return true
	}

	for nid, t := range s.tasks {
		if t.Status != "pending" {
			continue
		}
		if claimed[t.ID] {
			continue
		}
		ref := taskRef{
			TaskID: t.ID, NodeID: nid, Track: t.Track, Status: t.Status,
			Blocks: blocks[nid],
		}
		if depsSatisfied(nid) {
			unblocked = append(unblocked, ref)
		} else {
			blocked = append(blocked, ref)
		}
	}
	return unblocked, blocked
}

// parallelLayers returns the layered topological sort over pending+
// unclaimed Tasks. notScheduled holds Tasks that couldn't be placed
// (cycles or blocked-by-non-pending).
func (s *stateSnapshot) parallelLayers() (layers [][]taskRef, notScheduled []taskRef) {
	claimed := s.claimedTaskIDs()
	deps := s.dependsOn()

	pool := map[int]taskInfo{}
	for nid, t := range s.tasks {
		if t.Status == "pending" && !claimed[t.ID] {
			pool[nid] = t
		}
	}
	placed := map[int]bool{}

	depsSatisfied := func(nid int) bool {
		for _, dep := range deps[nid] {
			d, ok := s.tasks[dep]
			if !ok {
				continue
			}
			if d.Status == "done" || d.Status == "cancelled" {
				continue
			}
			if placed[dep] {
				continue
			}
			return false
		}
		return true
	}

	for len(pool) > 0 {
		var layer []taskRef
		var layerNids []int
		for nid := range pool {
			if depsSatisfied(nid) {
				layerNids = append(layerNids, nid)
			}
		}
		if len(layerNids) == 0 {
			break
		}
		sort.Ints(layerNids)
		for _, nid := range layerNids {
			t := pool[nid]
			layer = append(layer, taskRef{TaskID: t.ID, NodeID: nid, Track: t.Track, Status: t.Status})
			placed[nid] = true
			delete(pool, nid)
		}
		layers = append(layers, layer)
	}

	for nid, t := range pool {
		notScheduled = append(notScheduled, taskRef{TaskID: t.ID, NodeID: nid, Track: t.Track, Status: t.Status})
	}
	return layers, notScheduled
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func defaultAgentID() string {
	hostname, _ := os.Hostname()
	hostname = strings.ToLower(strings.TrimSuffix(hostname, ".local"))
	return fmt.Sprintf("agent-%s-mcp-%d", hostname, os.Getpid())
}
