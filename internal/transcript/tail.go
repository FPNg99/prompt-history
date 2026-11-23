package transcript

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Tailer follows a transcript file, feeding each newly completed line to a
// parser.
type Tailer struct {
	Path    string
	Parser  *Parser
	offset  int64
	partial []byte
}

// NewTailer returns a tailer positioned at the start of path.
func NewTailer(path string) *Tailer {
	return &Tailer{Path: path, Parser: NewParser()}
}

// Poll reads whatever was appended since the last call and reports whether
// any turn changed. A file that shrank is read again from the start.
func (t *Tailer) Poll() (bool, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	changed := false
	if info.Size() < t.offset {
		t.offset, t.partial, t.Parser = 0, nil, NewParser()
		changed = true
	}
	if info.Size() == t.offset {
		return changed, nil
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return changed, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return changed, err
	}
	t.offset += int64(len(data))
	data = append(t.partial, data...)
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		if t.Parser.Feed(data[:i]) {
			changed = true
		}
		data = data[i+1:]
	}
	// A last line without its newline is still being written.
	t.partial = append([]byte(nil), data...)
	return changed, nil
}

var sessionIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Locate finds the transcript file of a Claude Code session by its id.
//
// It looks under $CLAUDE_CONFIG_DIR and every ~/.claude* directory, since a
// machine can hold several Claude config directories, and returns the most
// recently written match.
func Locate(sessionID string) (string, error) {
	if !sessionIDRE.MatchString(sessionID) {
		return "", fmt.Errorf("not a session id: %q", sessionID)
	}
	var roots []string
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		roots = append(roots, dir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		more, _ := filepath.Glob(filepath.Join(home, ".claude*"))
		roots = append(roots, more...)
		roots = append(roots, filepath.Join(home, ".config", "claude"))
	}
	type found struct {
		path string
		mod  int64
	}
	var hits []found
	seen := map[string]bool{}
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "projects", "*", sessionID+".jsonl"))
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			if info, err := os.Stat(m); err == nil && !info.IsDir() {
				hits = append(hits, found{m, info.ModTime().UnixNano()})
			}
		}
	}
	if len(hits) == 0 {
		return "", fmt.Errorf("no transcript found for session %s", sessionID)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].mod > hits[j].mod })
	return hits[0].path, nil
}
