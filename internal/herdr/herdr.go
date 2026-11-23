// Package herdr asks the running Herdr session about panes through the
// `herdr` CLI. It only reads, except for opening this plugin's own pane and
// showing a notification.
package herdr

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PluginID is this plugin's id in herdr-plugin.toml.
const PluginID = "prompt-history"

// Pane is what Herdr knows about one pane.
type Pane struct {
	ID        string
	TabID     string
	Agent     string // "claude", or "" when no agent runs in it
	Status    string // idle, working, blocked, done, unknown
	Cwd       string
	SessionID string // the agent's own session id, when Herdr has learned it
}

func bin() string {
	if b := os.Getenv("HERDR_BIN_PATH"); b != "" {
		return b
	}
	return "herdr"
}

type rawPane struct {
	PaneID       string `json:"pane_id"`
	TabID        string `json:"tab_id"`
	Agent        string `json:"agent"`
	AgentStatus  string `json:"agent_status"`
	Cwd          string `json:"cwd"`
	AgentSession *struct {
		Value string `json:"value"`
	} `json:"agent_session"`
}

func (r rawPane) pane() Pane {
	p := Pane{ID: r.PaneID, TabID: r.TabID, Agent: r.Agent, Status: r.AgentStatus, Cwd: r.Cwd}
	if r.AgentSession != nil {
		p.SessionID = r.AgentSession.Value
	}
	return p
}

// GetPane reads one pane's state.
func GetPane(id string) (Pane, error) {
	out, err := exec.Command(bin(), "pane", "get", id).Output()
	if err != nil {
		return Pane{}, fmt.Errorf("herdr pane get %s: %w", id, err)
	}
	var resp struct {
		Result struct {
			Pane *rawPane `json:"pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || resp.Result.Pane == nil {
		return Pane{}, fmt.Errorf("herdr pane get %s: unexpected reply", id)
	}
	return resp.Result.Pane.pane(), nil
}

// ClaudePanesInTab lists the panes of a tab that run Claude Code.
func ClaudePanesInTab(workspaceID, tabID string) ([]Pane, error) {
	args := []string{"pane", "list"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	out, err := exec.Command(bin(), args...).Output()
	if err != nil {
		return nil, fmt.Errorf("herdr pane list: %w", err)
	}
	var resp struct {
		Result struct {
			Panes []rawPane `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("herdr pane list: unexpected reply")
	}
	var panes []Pane
	for _, r := range resp.Result.Panes {
		if r.Agent == "claude" && r.TabID == tabID {
			panes = append(panes, r.pane())
		}
	}
	return panes, nil
}

// Context is where an action was invoked: Herdr passes it as JSON in
// HERDR_PLUGIN_CONTEXT_JSON.
type Context struct {
	PaneID      string `json:"focused_pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

// ActionContext reads the invocation context, falling back to the pane
// variables Herdr sets for every managed process.
func ActionContext() Context {
	var c Context
	if raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	if c.PaneID == "" {
		c.PaneID = os.Getenv("HERDR_PANE_ID")
	}
	if c.TabID == "" {
		c.TabID = os.Getenv("HERDR_TAB_ID")
	}
	if c.WorkspaceID == "" {
		c.WorkspaceID = os.Getenv("HERDR_WORKSPACE_ID")
	}
	return c
}

// OpenViewer opens this plugin's viewer pane as a popup, passing env to it.
func OpenViewer(env map[string]string) error {
	args := []string{
		"plugin", "pane", "open",
		"--plugin", PluginID,
		"--entrypoint", "viewer",
		"--placement", "popup",
		"--width", "92%",
		"--height", "92%",
		"--focus",
	}
	for k, v := range env {
		if v != "" {
			args = append(args, "--env", k+"="+v)
		}
	}
	out, err := exec.Command(bin(), args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("herdr plugin pane open: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Notify shows a Herdr notification.
func Notify(title, body string) {
	args := []string{"notification", "show", title, "--sound", "none"}
	if body != "" {
		args = append(args, "--body", body)
	}
	_ = exec.Command(bin(), args...).Run()
}
