// prompt-history lists the prompts of a Claude Code session and shows
// the report Claude gave for each one. It runs as a Herdr plugin.
//
//	open      the plugin action: find the focused pane's Claude session and
//	          open the viewer for it in a popup
//	open-tab  the same, in a tab of its own; a second press goes back to
//	          the tab already open for that session
//	view   the popup itself
//	dump   print the prompt list, or one report, as plain text
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"prompt-history/internal/herdr"
	"prompt-history/internal/transcript"
	"prompt-history/internal/ui"
)

const title = "Prompt history"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "open":
		err = runOpen(herdr.PlacePopup)
	case "open-tab":
		err = runOpen(herdr.PlaceTab)
	case "view":
		err = runView(os.Args[2:])
	case "dump":
		err = runDump(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "prompt-history:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  prompt-history open | open-tab
  prompt-history view [--session ID | --file PATH] [--pane PANE_ID]
  prompt-history dump [--session ID | --file PATH] [--report N [--part K | --full]]
`)
}

// tabRecord is where the viewer tab opened for a session is remembered, so a
// second press of the key returns to it.
func tabRecord(sessionID string) string {
	root := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if root == "" {
		root = filepath.Join(os.TempDir(), "prompt-history")
	}
	return filepath.Join(root, "tabs", sessionID+".pane")
}

// sessionName is a short name for the Claude session in a pane: the title
// Claude Code gave it, else the start of its id.
func sessionName(pane herdr.Pane) string {
	name := pane.Title
	if name == "" || strings.EqualFold(name, "claude") || strings.EqualFold(name, "claude code") {
		name = "session " + clip(pane.SessionID, 9)
	}
	return name
}

// tabLabel names the viewer tab after the session it shows, so several
// history tabs can be told apart.
func tabLabel(pane herdr.Pane) string {
	return "history: " + clip(sessionName(pane), 40)
}

// runOpen is the plugin action. Problems are shown as a Herdr notification,
// since an action has no terminal of its own.
func runOpen(placement string) error {
	fail := func(body string) error {
		herdr.Notify(title, body)
		return errors.New(body)
	}
	ctx := herdr.ActionContext()
	if ctx.PaneID == "" {
		return fail("No pane is focused.")
	}
	pane, err := herdr.GetPane(ctx.PaneID)
	if err != nil {
		return fail(err.Error())
	}
	if pane.Agent != "claude" {
		// Focus is on a neighbour, such as an editor sidebar: use the tab's
		// Claude pane when there is exactly one.
		if pane.TabID != "" {
			ctx.TabID = pane.TabID
		}
		others, err := herdr.ClaudePanesInTab(ctx.WorkspaceID, ctx.TabID)
		switch {
		case err != nil:
			return fail(err.Error())
		case len(others) == 0:
			return fail("No pane in this tab is running Claude Code.")
		case len(others) > 1:
			return fail("Several panes in this tab run Claude Code. Focus the one you want first.")
		}
		pane = others[0]
	}
	if pane.SessionID == "" {
		return fail("Herdr does not know this pane's Claude session yet. Send a prompt first.")
	}
	if _, err := transcript.Locate(pane.SessionID); err != nil {
		return fail(err.Error())
	}
	record := tabRecord(pane.SessionID)
	if placement == herdr.PlaceTab {
		// Pane ids are never reused, so a recorded pane that still exists is
		// this session's viewer tab.
		if raw, err := os.ReadFile(record); err == nil {
			if id := strings.TrimSpace(string(raw)); id != "" {
				if viewer, err := herdr.GetPane(id); err == nil {
					// The session may have been renamed since the tab opened.
					herdr.RenameTab(viewer.TabID, tabLabel(pane))
					// Switch to its tab explicitly, then put focus on the pane.
					herdr.FocusTab(viewer.TabID)
					_ = herdr.FocusPluginPane(id)
					return nil
				}
			}
		}
	}
	paneID, tabID, err := herdr.OpenViewer(placement, map[string]string{
		"PH_SESSION_ID": pane.SessionID,
		"PH_PANE_ID":    pane.ID,
		"PH_PLACEMENT":  placement,
		"PH_TITLE":      sessionName(pane),
	})
	if err != nil {
		return fail(err.Error())
	}
	if placement == herdr.PlaceTab && paneID != "" {
		if tabID != "" {
			herdr.RenameTab(tabID, tabLabel(pane))
			// Opening with --focus should already land here; switching
			// explicitly makes sure the key always ends on the new tab.
			herdr.FocusTab(tabID)
		}
		if os.MkdirAll(filepath.Dir(record), 0o700) == nil {
			_ = os.WriteFile(record, []byte(paneID+"\n"), 0o600)
		}
	}
	return nil
}

// resolve finds the transcript from flags, falling back to the environment
// the open action passes to the popup.
func resolve(session, file string) (path, sessionID string, err error) {
	if file != "" {
		return file, strings.TrimSuffix(baseName(file), ".jsonl"), nil
	}
	if session == "" {
		session = os.Getenv("PH_SESSION_ID")
	}
	if session == "" {
		return "", "", errors.New("no session: pass --session or --file")
	}
	path, err = transcript.Locate(session)
	return path, session, err
}

func baseName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

func runView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ExitOnError)
	session := fs.String("session", "", "Claude session id")
	file := fs.String("file", "", "transcript file")
	pane := fs.String("pane", os.Getenv("PH_PANE_ID"), "the Claude pane, for its status")
	fs.Parse(args)

	path, sessionID, err := resolve(*session, *file)
	if err != nil {
		// The popup closes as soon as this exits: keep the reason on screen.
		fmt.Fprintln(os.Stderr, "prompt-history:", err)
		fmt.Fprint(os.Stderr, "\nPress Enter to close.")
		fmt.Scanln()
		return err
	}
	return ui.Run(ui.Options{
		Path:      path,
		SessionID: sessionID,
		PaneID:    *pane,
		Title:     os.Getenv("PH_TITLE"),
		Tab:       os.Getenv("PH_PLACEMENT") == herdr.PlaceTab,
		Config:    ui.LoadConfig(),
		Dark:      lipgloss.HasDarkBackground(),
	})
}

func runDump(args []string) error {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	session := fs.String("session", "", "Claude session id")
	file := fs.String("file", "", "transcript file")
	report := fs.Int("report", 0, "print this prompt's report instead of the list")
	full := fs.Bool("full", false, "with --report: the full turn, not only the final answer")
	part := fs.Int("part", 0, "with --report: which of the prompt's reports (default: the latest)")
	fs.Parse(args)

	path, _, err := resolve(*session, *file)
	if err != nil {
		return err
	}
	tail := transcript.NewTailer(path)
	if _, err := tail.Poll(); err != nil {
		return err
	}
	turns := tail.Parser.Turns()
	if *report > 0 {
		if *report > len(turns) {
			return fmt.Errorf("no prompt #%d (the session has %d)", *report, len(turns))
		}
		t := turns[*report-1]
		reports := t.Reports()
		switch {
		case *full:
			fmt.Println(t.FullAnswer())
		case *part > len(reports):
			return fmt.Errorf("prompt #%d has %d reports", *report, len(reports))
		case *part > 0:
			fmt.Println(reports[*part-1].Text)
		default:
			fmt.Println(t.FinalAnswer())
		}
		return nil
	}
	for _, t := range turns {
		clock := "     "
		if !t.Time.IsZero() {
			clock = t.Time.Local().Format("15:04")
		}
		fmt.Printf("#%-3d %s  %-7s %3d tools %2d files %2d reports  %s\n",
			t.N, clock, t.Kind, t.Tools, len(t.Files), len(t.Reports()), clip(t.FirstLine(), 80))
	}
	return nil
}

func clip(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}
