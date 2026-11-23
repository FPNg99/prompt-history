package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"herdr-prompt-history/internal/transcript"
)

// Config is the optional config.toml in the plugin's config directory.
type Config struct {
	// NvimBin is the editor binary reports open in. Default: nvim.
	NvimBin string `toml:"nvim_bin"`
	// NvimEnv is extra KEY=VALUE environment for it, e.g. NVIM_APPNAME=myapp.
	NvimEnv []string `toml:"nvim_env"`
}

// LoadConfig reads config.toml from HERDR_PLUGIN_CONFIG_DIR; a missing or
// malformed file gives the defaults.
func LoadConfig() Config {
	var cfg Config
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		_, _ = toml.DecodeFile(filepath.Join(dir, "config.toml"), &cfg)
	}
	return cfg
}

// editor picks the program a report opens in: the configured binary, nvim,
// $VISUAL, $EDITOR, then vim.
func (c Config) editor() (string, error) {
	candidates := []string{c.NvimBin, "nvim", os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vim"}
	for _, name := range candidates {
		if name == "" {
			continue
		}
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no editor found (tried nvim, $VISUAL, $EDITOR, vim)")
}

// turnStats is the one-line summary under a prompt: tools, files, duration.
func turnStats(t *transcript.Turn) string {
	var parts []string
	if t.Tools > 0 {
		parts = append(parts, plural(t.Tools, "tool"))
	}
	if n := len(t.Files); n > 0 {
		parts = append(parts, plural(n, "file"))
	}
	if d := t.Duration(); d > 0 {
		parts = append(parts, shortDuration(d))
	}
	if len(parts) == 0 {
		return "no answer yet"
	}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func shortDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func kindLabel(kind string) string {
	switch kind {
	case transcript.KindQueued:
		return "sent while Claude was working"
	case transcript.KindReject:
		return "sent when declining a tool call"
	case transcript.KindCommand:
		return "slash command"
	}
	return ""
}

// answerOf is the turn's answer in the chosen mode.
func answerOf(t *transcript.Turn, full bool) string {
	if full {
		return t.FullAnswer()
	}
	return t.FinalAnswer()
}

// reportMarkdown is the document a report opens as in the editor: the prompt
// quoted on top, Claude's answer below.
func reportMarkdown(t *transcript.Turn, full bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Prompt #%d\n\n", t.N)
	meta := []string{}
	if !t.Time.IsZero() {
		meta = append(meta, t.Time.Local().Format("2006-01-02 15:04"))
	}
	if label := kindLabel(t.Kind); label != "" {
		meta = append(meta, label)
	}
	meta = append(meta, turnStats(t))
	if full {
		meta = append(meta, "full turn")
	}
	b.WriteString(strings.Join(meta, " · ") + "\n\n")
	for _, line := range strings.Split(t.Prompt, "\n") {
		b.WriteString("> " + line + "\n")
	}
	b.WriteString("\n---\n\n")
	answer := answerOf(t, full)
	if strings.TrimSpace(answer) == "" {
		answer = "_No answer yet._"
	}
	b.WriteString(answer + "\n")
	return b.String()
}

// writeReport saves a turn's report as markdown and returns the file's path.
func writeReport(sessionID string, t *transcript.Turn, full bool) (string, error) {
	root := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if root == "" {
		root = filepath.Join(os.TempDir(), "herdr-prompt-history")
	}
	if sessionID == "" {
		sessionID = "session"
	}
	dir := filepath.Join(root, sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("report-%03d.md", t.N)
	if full {
		name = fmt.Sprintf("report-%03d-full.md", t.N)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(reportMarkdown(t, full)), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// copyToClipboard puts text on the system clipboard.
func copyToClipboard(text string) error {
	var candidates [][]string
	if runtime.GOOS == "darwin" {
		candidates = [][]string{{"pbcopy"}}
	} else {
		candidates = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
	for _, c := range candidates {
		path, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return fmt.Errorf("no clipboard tool found")
}
