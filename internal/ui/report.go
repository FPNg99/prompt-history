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

	"prompt-history/internal/transcript"
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
	if n := len(t.Reports()); n > 1 {
		parts = append(parts, plural(n, "report"))
	}
	if n := t.Usage.Total(); n > 0 {
		parts = append(parts, compact(n)+" tok")
	}
	if d := t.Duration(); d > 0 {
		parts = append(parts, shortDuration(d))
	}
	if len(parts) == 0 {
		return "no answer yet"
	}
	return strings.Join(parts, " · ")
}

// listStats is the line under a prompt in the list: the model it was sent
// to, then its stats.
func listStats(t *transcript.Turn) string {
	if model := shortModel(t.Model()); model != "" {
		return model + " · " + turnStats(t)
	}
	return turnStats(t)
}

// compact writes a token count the short way: 950, 12.3k, 1.9M.
func compact(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 999_950:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e3), ".0") + "k"
	default:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	}
}

// tokenLine spells out a turn's token use, or "" when none is recorded.
func tokenLine(t *transcript.Turn) string {
	u := t.Usage
	if u.Total() == 0 {
		return ""
	}
	line := fmt.Sprintf("tokens: %s in", compact(u.TotalInput()))
	if u.CacheRead > 0 {
		line += fmt.Sprintf(" (%s from cache)", compact(u.CacheRead))
	}
	return line + fmt.Sprintf(" · %s out", compact(u.Output))
}

// shortModel drops the vendor prefix, which every row would repeat.
func shortModel(model string) string {
	return strings.TrimPrefix(model, "claude-")
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

// answerOf is what the report pane shows: the whole turn, or one of its
// reports by index.
func answerOf(t *transcript.Turn, full bool, part int) string {
	if full {
		return t.FullAnswer()
	}
	reports := t.Reports()
	if part < 0 || part >= len(reports) {
		return ""
	}
	return reports[part].Text
}

// modelOf is the model behind what the report pane shows: the picked
// report's, or for the full turn every model that took part.
func modelOf(t *transcript.Turn, full bool, part int) string {
	if full {
		return strings.Join(t.Models(), ", ")
	}
	reports := t.Reports()
	if part < 0 || part >= len(reports) {
		return ""
	}
	return reports[part].Model
}

// partLabel names the shown report, e.g. "report 2 of 5" or, for the last
// one, "report 5 of 5 · latest".
func partLabel(t *transcript.Turn, full bool, part int) string {
	if full {
		return "full turn"
	}
	total := len(t.Reports())
	switch {
	case total == 0:
		return ""
	case total == 1:
		return "final answer"
	case part == total-1:
		return fmt.Sprintf("report %d of %d · latest", part+1, total)
	}
	return fmt.Sprintf("report %d of %d", part+1, total)
}

// reportMarkdown is the document a report opens as in the editor: the prompt
// quoted on top, Claude's answer below.
func reportMarkdown(t *transcript.Turn, full bool, part int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Prompt #%d\n\n", t.N)
	meta := []string{}
	if !t.Time.IsZero() {
		meta = append(meta, t.Time.Local().Format("2006-01-02 15:04"))
	}
	if label := kindLabel(t.Kind); label != "" {
		meta = append(meta, label)
	}
	if model := t.Model(); model != "" {
		meta = append(meta, "prompted with "+model)
	}
	meta = append(meta, turnStats(t))
	if tokens := tokenLine(t); tokens != "" {
		meta = append(meta, tokens)
	}
	if label := partLabel(t, full, part); label != "" {
		meta = append(meta, label)
	}
	if model := modelOf(t, full, part); model != "" {
		meta = append(meta, model)
	}
	b.WriteString(strings.Join(meta, " · ") + "\n\n")
	for _, line := range strings.Split(t.Prompt, "\n") {
		b.WriteString("> " + line + "\n")
	}
	b.WriteString("\n---\n\n")
	answer := answerOf(t, full, part)
	if strings.TrimSpace(answer) == "" {
		answer = "_No answer yet._"
	}
	b.WriteString(answer + "\n")
	return b.String()
}

// writeReport saves a turn's report as markdown and returns the file's path.
func writeReport(sessionID string, t *transcript.Turn, full bool, part int) (string, error) {
	root := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if root == "" {
		root = filepath.Join(os.TempDir(), "prompt-history")
	}
	if sessionID == "" {
		sessionID = "session"
	}
	dir := filepath.Join(root, sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("report-%03d-%02d.md", t.N, part+1)
	if full {
		name = fmt.Sprintf("report-%03d-full.md", t.N)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(reportMarkdown(t, full, part)), 0o600); err != nil {
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
