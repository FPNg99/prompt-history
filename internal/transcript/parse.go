// Package transcript reads a Claude Code session transcript (JSONL) and
// groups it into turns: one prompt the person sent plus everything Claude
// answered until the next prompt.
//
// The transcript format is not documented, so the parser is defensive:
// unknown entry types are skipped and every field is optional.
package transcript

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// ItemKind says what one piece of Claude's answer is.
type ItemKind int

const (
	// ItemText is a text block Claude wrote.
	ItemText ItemKind = iota
	// ItemTool is a tool call Claude made.
	ItemTool
)

// Item is one piece of a turn's answer, in the order Claude produced it.
type Item struct {
	Kind    ItemKind
	Time    time.Time
	Model   string // the model that produced it, when the transcript says
	Text    string // ItemText: the markdown text
	Tool    string // ItemTool: the tool's name
	Summary string // ItemTool: a one-line summary of its input
}

// Kinds of prompt a turn can open with.
const (
	KindPrompt  = "prompt"  // typed at the prompt
	KindCommand = "command" // a slash command
	KindQueued  = "queued"  // typed while Claude was working
	KindReject  = "reject"  // typed when declining a tool call
)

// Turn is one prompt and Claude's answer to it.
type Turn struct {
	N      int // 1-based position among the visible turns
	Kind   string
	Time   time.Time // when the prompt was sent
	End    time.Time // when the last piece of the answer was written
	Prompt string
	Items  []Item
	Tools  int
	Files  []string // distinct files edited or written, in first-seen order
	Usage  Usage    // tokens spent answering, summed over the turn's requests
}

// Usage counts tokens as the API reports them for each request.
type Usage struct {
	Input      int // input tokens read fresh, not from the cache
	CacheRead  int // input tokens read from the prompt cache
	CacheWrite int // input tokens written to the prompt cache
	Output     int // tokens Claude wrote, thinking included
}

// TotalInput is everything sent to the model: fresh, cached and newly cached.
func (u Usage) TotalInput() int { return u.Input + u.CacheRead + u.CacheWrite }

// Total is all tokens, in and out.
func (u Usage) Total() int { return u.TotalInput() + u.Output }

// Report is one thing Claude told the person during a turn: a run of text
// blocks with no tool call in between. A turn with tool calls usually has
// several, the last being its closing answer.
type Report struct {
	N           int       // 1-based position in the turn
	Time        time.Time // when Claude started writing it
	Text        string
	Model       string // the model that wrote it
	ToolsBefore int    // tool calls made since the previous report
}

// Reports splits the turn's answer at its tool calls.
func (t *Turn) Reports() []Report {
	var out []Report
	tools := 0
	open := false
	for _, it := range t.Items {
		if it.Kind == ItemTool {
			tools++
			open = false
			continue
		}
		if open {
			out[len(out)-1].Text += "\n\n" + it.Text
			continue
		}
		out = append(out, Report{N: len(out) + 1, Time: it.Time, Text: it.Text, Model: it.Model, ToolsBefore: tools})
		tools = 0
		open = true
	}
	return out
}

// Models lists the distinct models that answered the turn, in first-used
// order: more than one when the model was switched part-way.
func (t *Turn) Models() []string {
	var out []string
	for _, it := range t.Items {
		if it.Model != "" && !contains(out, it.Model) {
			out = append(out, it.Model)
		}
	}
	return out
}

// Model is the model the prompt was sent to: the one that began answering
// it. Empty until the answer starts.
func (t *Turn) Model() string {
	if models := t.Models(); len(models) > 0 {
		return models[0]
	}
	return ""
}

// FinalAnswer is the text Claude closed the turn with: its last report,
// ignoring any tool calls that follow it.
func (t *Turn) FinalAnswer() string {
	reports := t.Reports()
	if len(reports) == 0 {
		return ""
	}
	return reports[len(reports)-1].Text
}

// FullAnswer is every text block of the turn in order, with each run of tool
// calls shown as a short list between them.
func (t *Turn) FullAnswer() string {
	var b strings.Builder
	inTools := false
	for _, it := range t.Items {
		switch it.Kind {
		case ItemText:
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(it.Text)
			inTools = false
		case ItemTool:
			if !inTools && b.Len() > 0 {
				b.WriteString("\n\n")
			} else if inTools {
				b.WriteString("\n")
			}
			// One code span, so markdown leaves paths and globs alone.
			line := it.Tool
			if it.Summary != "" {
				line += "  " + strings.ReplaceAll(it.Summary, "`", "'")
			}
			b.WriteString("- `" + line + "`")
			inTools = true
		}
	}
	return b.String()
}

// Duration is how long the answer took, or zero when nothing was answered.
func (t *Turn) Duration() time.Duration {
	if t.End.IsZero() || t.Time.IsZero() || t.End.Before(t.Time) {
		return 0
	}
	return t.End.Sub(t.Time)
}

// FirstLine is the prompt's first non-empty line.
func (t *Turn) FirstLine() string {
	for _, line := range strings.Split(t.Prompt, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// Parser builds turns from transcript lines fed in file order.
type Parser struct {
	turns []*Turn
	seen  map[string]bool
	// counted holds the API message ids whose usage is already added: one
	// message is stored as several entries, each repeating its usage.
	counted map[string]bool
}

// NewParser returns an empty parser.
func NewParser() *Parser {
	return &Parser{seen: map[string]bool{}, counted: map[string]bool{}}
}

// Turns returns the turns worth showing, numbered from 1.
//
// A slash command Claude never answered (a local one such as /model) is left
// out, and so is an unanswered prompt immediately repeated by the next turn.
func (p *Parser) Turns() []*Turn {
	out := make([]*Turn, 0, len(p.turns))
	for i, t := range p.turns {
		if len(t.Items) == 0 {
			if t.Kind == KindCommand {
				continue
			}
			if i+1 < len(p.turns) && p.turns[i+1].Prompt == t.Prompt {
				continue
			}
		}
		out = append(out, t)
	}
	for i, t := range out {
		t.N = i + 1
	}
	return out
}

type origin struct {
	Kind string `json:"kind"`
}

type entry struct {
	Type             string  `json:"type"`
	UUID             string  `json:"uuid"`
	IsMeta           bool    `json:"isMeta"`
	IsSidechain      bool    `json:"isSidechain"`
	IsCompactSummary bool    `json:"isCompactSummary"`
	Timestamp        string  `json:"timestamp"`
	ThinkingDisplay  string  `json:"thinkingDisplay"`
	Origin           *origin `json:"origin"`
	Message          *struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input      int `json:"input_tokens"`
			Output     int `json:"output_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Attachment *struct {
		Type        string          `json:"type"`
		Prompt      json.RawMessage `json:"prompt"`
		CommandMode string          `json:"commandMode"`
		Timestamp   string          `json:"timestamp"`
		Origin      *origin         `json:"origin"`
	} `json:"attachment"`
}

type block struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
}

var relevant = [][]byte{
	[]byte(`"type":"user"`),
	[]byte(`"type":"assistant"`),
	[]byte(`"type":"attachment"`),
}

// Feed parses one transcript line and reports whether it changed any turn.
func (p *Parser) Feed(line []byte) bool {
	// Most lines are bookkeeping, some of them very large: skip those
	// without decoding them.
	hit := false
	for _, needle := range relevant {
		if bytes.Contains(line, needle) {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	var e entry
	if err := json.Unmarshal(line, &e); err != nil {
		return false
	}
	if e.IsSidechain {
		return false
	}
	if e.UUID != "" {
		if p.seen[e.UUID] {
			return false
		}
		p.seen[e.UUID] = true
	}
	ts := parseTime(e.Timestamp)
	switch e.Type {
	case "user":
		return p.feedUser(&e, ts)
	case "assistant":
		return p.feedAssistant(&e, ts)
	case "attachment":
		return p.feedAttachment(&e, ts)
	}
	return false
}

func (p *Parser) start(kind, prompt string, ts time.Time) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	p.turns = append(p.turns, &Turn{Kind: kind, Prompt: prompt, Time: ts})
	return true
}

func (p *Parser) feedUser(e *entry, ts time.Time) bool {
	if e.Message == nil || e.IsMeta || e.IsCompactSummary {
		return false
	}
	if e.Origin != nil && e.Origin.Kind != "" && e.Origin.Kind != "human" {
		return false
	}
	text, blocks := decodeContent(e.Message.Content)
	changed := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if text != "" {
				text += "\n"
			}
			text += b.Text
		case "tool_result":
			if said := rejectionMessage(b.Content); said != "" {
				changed = p.start(KindReject, said, ts) || changed
			}
		}
	}
	kind, prompt := classifyPrompt(text)
	if kind == "" {
		return changed
	}
	return p.start(kind, prompt, ts) || changed
}

func (p *Parser) feedAttachment(e *entry, ts time.Time) bool {
	a := e.Attachment
	if a == nil || a.Type != "queued_command" {
		return false
	}
	if a.CommandMode != "" && a.CommandMode != "prompt" {
		return false
	}
	if a.Origin != nil && a.Origin.Kind != "" && a.Origin.Kind != "human" {
		return false
	}
	text, blocks := decodeContent(a.Prompt)
	for _, b := range blocks {
		if b.Type == "text" {
			if text != "" {
				text += "\n"
			}
			text += b.Text
		}
	}
	if at := parseTime(a.Timestamp); !at.IsZero() {
		ts = at
	}
	kind, prompt := classifyPrompt(text)
	if kind == "" {
		return false
	}
	if kind == KindPrompt {
		kind = KindQueued
	}
	return p.start(kind, prompt, ts)
}

func (p *Parser) feedAssistant(e *entry, ts time.Time) bool {
	if e.Message == nil || len(p.turns) == 0 {
		return false
	}
	t := p.turns[len(p.turns)-1]
	model := e.Message.Model
	if strings.HasPrefix(model, "<") {
		// A placeholder such as "<synthetic>": the harness wrote the
		// message, no model did.
		model = ""
	}
	_, blocks := decodeContent(e.Message.Content)
	changed := false
	if u := e.Message.Usage; u != nil && model != "" && e.Message.ID != "" && !p.counted[e.Message.ID] {
		p.counted[e.Message.ID] = true
		t.Usage.Input += u.Input
		t.Usage.Output += u.Output
		t.Usage.CacheRead += u.CacheRead
		t.Usage.CacheWrite += u.CacheWrite
		changed = true
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			t.Items = append(t.Items, Item{Kind: ItemText, Time: ts, Model: model, Text: strings.TrimSpace(b.Text)})
			changed = true
		case "thinking":
			// In "updates" display mode Claude Code stores a progress
			// message written just before a tool call as a thinking block
			// with text. Real reasoning is stored empty, and in any other
			// mode a thinking block is not something the person was told.
			if e.ThinkingDisplay != "updates" || strings.TrimSpace(b.Thinking) == "" {
				continue
			}
			t.Items = append(t.Items, Item{Kind: ItemText, Time: ts, Model: model, Text: strings.TrimSpace(b.Thinking)})
			changed = true
		case "tool_use":
			summary, file := summarizeInput(b.Name, b.Input)
			t.Items = append(t.Items, Item{Kind: ItemTool, Time: ts, Model: model, Tool: b.Name, Summary: summary})
			t.Tools++
			if file != "" && !contains(t.Files, file) {
				t.Files = append(t.Files, file)
			}
			changed = true
		}
	}
	if changed && !ts.IsZero() {
		t.End = ts
	}
	return changed
}

// decodeContent reads a message's content, which is either one string or a
// list of blocks.
func decodeContent(raw json.RawMessage) (string, []block) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) == nil {
		return "", blocks
	}
	return "", nil
}

var (
	reminderRE = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	cmdNameRE  = regexp.MustCompile(`(?s)<command-name>\s*(.*?)\s*</command-name>`)
	cmdArgsRE  = regexp.MustCompile(`(?s)<command-args>\s*(.*?)\s*</command-args>`)
)

// Prefixes of user entries the harness wrote, not the person.
var notPrompts = []string{
	"<local-command-caveat>",
	"<local-command-stdout>",
	"<local-command-stderr>",
	"<task-notification>",
	"<bash-input>",
	"<bash-stdout>",
	"<bash-stderr>",
	"[Request interrupted",
	"Caveat: The messages below were generated by the user while running local commands",
}

// classifyPrompt decides whether a user entry's text is something the person
// sent, returning its kind and display text, or "" when it is not.
func classifyPrompt(text string) (kind, prompt string) {
	text = strings.TrimSpace(reminderRE.ReplaceAllString(text, ""))
	if text == "" {
		return "", ""
	}
	for _, prefix := range notPrompts {
		if strings.HasPrefix(text, prefix) {
			return "", ""
		}
	}
	if m := cmdNameRE.FindStringSubmatch(text); m != nil {
		name := m[1]
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		if a := cmdArgsRE.FindStringSubmatch(text); a != nil && a[1] != "" {
			name += " " + a[1]
		}
		return KindCommand, name
	}
	return KindPrompt, text
}

const rejectMarker = "the user said:\n"

// rejectionMessage extracts what the person typed when declining a tool call;
// the harness stores it inside that tool's result.
func rejectionMessage(raw json.RawMessage) string {
	text, blocks := decodeContent(raw)
	for _, b := range blocks {
		if b.Type == "text" {
			text += b.Text
		}
	}
	if !strings.Contains(text, "doesn't want to proceed with this tool use") {
		return ""
	}
	i := strings.Index(text, rejectMarker)
	if i < 0 {
		return ""
	}
	said := text[i+len(rejectMarker):]
	if j := strings.Index(said, "\n\nNote: The user's next message"); j >= 0 {
		said = said[:j]
	}
	return strings.TrimSpace(said)
}

// Input fields tried, in order, for a tool call's one-line summary.
var summaryFields = []string{
	"file_path", "notebook_path", "path", "command", "pattern", "query",
	"url", "skill", "description", "prompt",
}

var fileTools = map[string]bool{
	"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true,
}

func summarizeInput(tool string, raw json.RawMessage) (summary, file string) {
	var in map[string]json.RawMessage
	if json.Unmarshal(raw, &in) != nil {
		return "", ""
	}
	str := func(key string) string {
		var s string
		if v, ok := in[key]; ok && json.Unmarshal(v, &s) == nil {
			return s
		}
		return ""
	}
	if fileTools[tool] {
		file = str("file_path")
		if file == "" {
			file = str("notebook_path")
		}
	}
	for _, key := range summaryFields {
		if s := str(key); s != "" {
			return oneLine(s, 100), file
		}
	}
	return "", file
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
