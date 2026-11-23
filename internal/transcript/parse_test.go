package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A small session covering every entry shape the parser must tell apart.
const fixture = `{"type":"mode","mode":"default","sessionId":"s"}
{"type":"user","uuid":"u0","isMeta":true,"timestamp":"2026-01-02T10:00:00.000Z","message":{"role":"user","content":"<local-command-caveat>The command below was run directly</local-command-caveat>"}}
{"type":"user","uuid":"u1","timestamp":"2026-01-02T10:00:00.000Z","message":{"role":"user","content":"<command-name>/model</command-name>\n<command-message>model</command-message>\n<command-args></command-args>"}}
{"type":"user","uuid":"u2","timestamp":"2026-01-02T10:00:01.000Z","message":{"role":"user","content":"<local-command-stdout>Set model</local-command-stdout>"}}
{"type":"user","uuid":"u3","origin":{"kind":"human"},"timestamp":"2026-01-02T10:01:00.000Z","message":{"role":"user","content":"<system-reminder>ignore me</system-reminder>\nfix the login bug\nit fails on Safari"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-01-02T10:01:05.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"}]}}
{"type":"assistant","uuid":"a2","timestamp":"2026-01-02T10:01:06.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Looking at the login code."}]}}
{"type":"assistant","uuid":"a3","timestamp":"2026-01-02T10:01:07.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/app/login.ts"}}]}}
{"type":"user","uuid":"u4","timestamp":"2026-01-02T10:01:08.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file body"}]}}
{"type":"assistant","uuid":"a4","timestamp":"2026-01-02T10:01:09.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/app/login.ts","old_string":"a","new_string":"b"}}]}}
{"type":"assistant","uuid":"a4b","isSidechain":true,"timestamp":"2026-01-02T10:01:09.500Z","message":{"role":"assistant","content":[{"type":"text","text":"subagent chatter"}]}}
{"type":"user","uuid":"u5","timestamp":"2026-01-02T10:01:10.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}
{"type":"assistant","uuid":"a5","timestamp":"2026-01-02T10:01:30.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Fixed the Safari bug."}]}}
{"type":"assistant","uuid":"a5","timestamp":"2026-01-02T10:01:30.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Fixed the Safari bug."}]}}
{"type":"assistant","uuid":"a6","timestamp":"2026-01-02T10:01:31.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Tests pass."}]}}
{"type":"user","uuid":"u6","origin":{"kind":"human"},"timestamp":"2026-01-02T10:05:00.000Z","message":{"role":"user","content":[{"type":"text","text":"now add a test"}]}}
{"type":"assistant","uuid":"a7","timestamp":"2026-01-02T10:05:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Adding one."}]}}
{"type":"assistant","uuid":"a8","timestamp":"2026-01-02T10:05:03.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t3","name":"Write","input":{"file_path":"/app/login.test.ts","content":"x"}}]}}
{"type":"attachment","uuid":"q1","timestamp":"2026-01-02T10:05:04.000Z","attachment":{"type":"queued_command","prompt":"use vitest please","commandMode":"prompt","origin":{"kind":"human"},"timestamp":"2026-01-02T10:05:03.500Z"}}
{"type":"assistant","uuid":"a9","timestamp":"2026-01-02T10:05:10.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Switched to vitest."}]}}
{"type":"assistant","uuid":"a10","timestamp":"2026-01-02T10:05:11.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t4","name":"ExitPlanMode","input":{}}]}}
{"type":"user","uuid":"u7","timestamp":"2026-01-02T10:06:00.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t4","is_error":true,"content":"The user doesn't want to proceed with this tool use. The tool use was rejected. To tell you how to proceed, the user said:\nalso cover the logout flow\n\nNote: The user's next message may contain a correction."}]}}
{"type":"assistant","uuid":"a11","timestamp":"2026-01-02T10:06:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Covered logout too."}]}}
{"type":"user","uuid":"u8","origin":{"kind":"task"},"timestamp":"2026-01-02T10:07:00.000Z","message":{"role":"user","content":"background task finished"}}
{"type":"user","uuid":"u9","isCompactSummary":true,"timestamp":"2026-01-02T10:08:00.000Z","message":{"role":"user","content":"This session is being continued from a previous conversation"}}
{"type":"user","uuid":"u10","timestamp":"2026-01-02T10:09:00.000Z","message":{"role":"user","content":"<command-name>/code-review</command-name>\n<command-args>high</command-args>"}}
{"type":"assistant","uuid":"a12","timestamp":"2026-01-02T10:09:20.000Z","message":{"role":"assistant","content":[{"type":"text","text":"No findings."}]}}
not json at all
`

func parseFixture(t *testing.T) []*Turn {
	t.Helper()
	p := NewParser()
	for _, line := range strings.Split(fixture, "\n") {
		p.Feed([]byte(line))
	}
	return p.Turns()
}

func TestTurns(t *testing.T) {
	turns := parseFixture(t)
	want := []struct {
		kind, prompt, final string
		tools, files        int
	}{
		{KindPrompt, "fix the login bug\nit fails on Safari", "Fixed the Safari bug.\n\nTests pass.", 2, 1},
		{KindPrompt, "now add a test", "Adding one.", 1, 1},
		{KindQueued, "use vitest please", "Switched to vitest.", 1, 0},
		{KindReject, "also cover the logout flow", "Covered logout too.", 0, 0},
		{KindCommand, "/code-review high", "No findings.", 0, 0},
	}
	if len(turns) != len(want) {
		for _, turn := range turns {
			t.Logf("turn %d %s %q", turn.N, turn.Kind, turn.Prompt)
		}
		t.Fatalf("got %d turns, want %d", len(turns), len(want))
	}
	for i, w := range want {
		got := turns[i]
		if got.N != i+1 {
			t.Errorf("turn %d: N = %d", i, got.N)
		}
		if got.Kind != w.kind || got.Prompt != w.prompt {
			t.Errorf("turn %d: got %s %q, want %s %q", i, got.Kind, got.Prompt, w.kind, w.prompt)
		}
		if final := got.FinalAnswer(); final != w.final {
			t.Errorf("turn %d: final = %q, want %q", i, final, w.final)
		}
		if got.Tools != w.tools || len(got.Files) != w.files {
			t.Errorf("turn %d: %d tools %d files, want %d and %d", i, got.Tools, len(got.Files), w.tools, w.files)
		}
	}
}

func TestTurnDetails(t *testing.T) {
	first := parseFixture(t)[0]
	if first.FirstLine() != "fix the login bug" {
		t.Errorf("first line = %q", first.FirstLine())
	}
	if d := first.Duration(); d != 31*time.Second {
		t.Errorf("duration = %v, want 31s", d)
	}
	full := first.FullAnswer()
	wantFull := "Looking at the login code.\n\n- `Read  /app/login.ts`\n- `Edit  /app/login.ts`\n\nFixed the Safari bug.\n\nTests pass."
	if full != wantFull {
		t.Errorf("full answer:\n%s\nwant:\n%s", full, wantFull)
	}
}

func TestReports(t *testing.T) {
	turns := parseFixture(t)
	reports := turns[0].Reports()
	want := []struct {
		text  string
		tools int
	}{
		{"Looking at the login code.", 0},
		{"Fixed the Safari bug.\n\nTests pass.", 2},
	}
	if len(reports) != len(want) {
		t.Fatalf("got %d reports, want %d", len(reports), len(want))
	}
	for i, w := range want {
		if reports[i].N != i+1 || reports[i].Text != w.text || reports[i].ToolsBefore != w.tools {
			t.Errorf("report %d = %+v, want %q after %d tools", i, reports[i], w.text, w.tools)
		}
	}
	if reports[1].Time.IsZero() {
		t.Error("report has no time")
	}
	// A turn that ends on a tool call still has its text as the last report.
	if got := turns[1].Reports(); len(got) != 1 || got[0].Text != "Adding one." {
		t.Errorf("turn ending on a tool call: %+v", got)
	}
}

func TestProgressUpdates(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-01-02T10:00:00.000Z","message":{"content":"do it"}}`,
		`{"type":"assistant","uuid":"a1","thinkingDisplay":"updates","timestamp":"2026-01-02T10:00:01.000Z","message":{"content":[{"type":"thinking","thinking":""}]}}`,
		`{"type":"assistant","uuid":"a2","thinkingDisplay":"updates","timestamp":"2026-01-02T10:00:02.000Z","message":{"content":[{"type":"thinking","thinking":"Checking the config first.\n\n"}]}}`,
		`{"type":"assistant","uuid":"a3","timestamp":"2026-01-02T10:00:03.000Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"assistant","uuid":"a4","thinkingDisplay":"summarized","timestamp":"2026-01-02T10:00:04.000Z","message":{"content":[{"type":"thinking","thinking":"private reasoning"}]}}`,
		`{"type":"assistant","uuid":"a5","timestamp":"2026-01-02T10:00:05.000Z","message":{"model":"model-b","content":[{"type":"text","text":"Done."}]}}`,
		`{"type":"assistant","uuid":"a6","timestamp":"2026-01-02T10:00:06.000Z","message":{"model":"<synthetic>","content":[{"type":"tool_use","name":"Bash","input":{}}]}}`,
	}
	p := NewParser()
	for _, line := range lines {
		p.Feed([]byte(line))
	}
	reports := p.Turns()[0].Reports()
	if len(reports) != 2 || reports[0].Text != "Checking the config first." || reports[1].Text != "Done." {
		t.Errorf("reports = %+v", reports)
	}
	if reports[0].Model != "" || reports[1].Model != "model-b" {
		t.Errorf("models = %q, %q", reports[0].Model, reports[1].Model)
	}
	if got := p.Turns()[0].Model(); got != "model-b" {
		t.Errorf("prompt model = %q", got)
	}
	if models := p.Turns()[0].Models(); len(models) != 1 || models[0] != "model-b" {
		t.Errorf("turn models = %v", models)
	}
}

func TestUsage(t *testing.T) {
	usage := `"usage":{"input_tokens":2,"output_tokens":100,"cache_read_input_tokens":1000,"cache_creation_input_tokens":50}`
	lines := []string{
		`{"type":"user","uuid":"u1","message":{"content":"do it"}}`,
		// One API message stored as two entries: its usage counts once.
		`{"type":"assistant","uuid":"a1","message":{"id":"m1","model":"model-a",` + usage + `,"content":[{"type":"thinking","thinking":""}]}}`,
		`{"type":"assistant","uuid":"a2","message":{"id":"m1","model":"model-a",` + usage + `,"content":[{"type":"tool_use","name":"Bash","input":{}}]}}`,
		`{"type":"assistant","uuid":"a3","message":{"id":"m2","model":"model-a",` + usage + `,"content":[{"type":"text","text":"Done."}]}}`,
		`{"type":"assistant","uuid":"a4","message":{"id":"m3","model":"<synthetic>",` + usage + `,"content":[{"type":"text","text":"note"}]}}`,
	}
	p := NewParser()
	for _, line := range lines {
		p.Feed([]byte(line))
	}
	got := p.Turns()[0].Usage
	want := Usage{Input: 4, Output: 200, CacheRead: 2000, CacheWrite: 100}
	if got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
	if got.TotalInput() != 2104 || got.Total() != 2304 {
		t.Errorf("totals = %d in, %d all", got.TotalInput(), got.Total())
	}
}

func TestTailerFollowsAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := strings.Split(strings.TrimSpace(fixture), "\n")
	half := strings.Join(lines[:8], "\n") + "\n"
	// End the first write mid-line: the tailer must wait for the rest.
	cut := len(lines[8]) / 2
	if err := os.WriteFile(path, []byte(half+lines[8][:cut]), 0o600); err != nil {
		t.Fatal(err)
	}
	tail := NewTailer(path)
	changed, err := tail.Poll()
	if err != nil || !changed {
		t.Fatalf("first poll: changed=%v err=%v", changed, err)
	}
	if n := len(tail.Parser.Turns()); n != 1 {
		t.Fatalf("after first poll: %d turns, want 1", n)
	}
	if changed, _ := tail.Poll(); changed {
		t.Error("poll with nothing new reported a change")
	}
	rest := lines[8][cut:] + "\n" + strings.Join(lines[9:], "\n") + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(rest)
	f.Close()
	if changed, err := tail.Poll(); err != nil || !changed {
		t.Fatalf("second poll: changed=%v err=%v", changed, err)
	}
	if n := len(tail.Parser.Turns()); n != 5 {
		t.Errorf("after second poll: %d turns, want 5", n)
	}
}

func TestLocate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dir := filepath.Join(home, ".claude-personal", "projects", "-some-project")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "abc-123.jsonl")
	if err := os.WriteFile(want, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Locate("abc-123")
	if err != nil || got != want {
		t.Errorf("Locate = %q, %v; want %q", got, err, want)
	}
	if _, err := Locate("missing"); err == nil {
		t.Error("Locate of an unknown session returned no error")
	}
	if _, err := Locate("../../etc/*"); err == nil {
		t.Error("Locate accepted a path as a session id")
	}
}
