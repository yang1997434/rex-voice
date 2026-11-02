package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// claudeSession is ~/.claude/sessions/<pid>.json, written by Claude Code itself. The format is not a
// public contract, so any missing field falls back to lsof / transcript mtime instead of failing.
type claudeSession struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

func (e *env) claudeSession(pid int) claudeSession {
	var s claudeSession
	if raw, err := os.ReadFile(filepath.Join(e.Home, ".claude", "sessions", strconv.Itoa(pid)+".json")); err == nil {
		_ = json.Unmarshal(raw, &s) // unparseable = absent, per the fallback rule above
	}
	return s
}

// cwdOf prefers the sessions file and falls back to lsof; "" when neither knows.
func (e *env) cwdOf(ctx context.Context, pid int) string {
	if s := e.claudeSession(pid); s.Cwd != "" {
		return s.Cwd
	}
	out, err := e.Exec(ctx, "lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn")
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "n") {
			return l[1:]
		}
	}
	return ""
}

var (
	psLine    = regexp.MustCompile(`^\s*(\d+)\s+(\S+ \S+\s+\d+ \S+ \d+)\s+(.*)$`)
	claudeCmd = regexp.MustCompile(`^(\S*/)?claude(\s|$)`)
	notClaude = regexp.MustCompile(`claude-|\.app/`)
)

type claudeProc struct {
	pid     int
	started string
	command string
}

func parsePS(out string) []claudeProc {
	var ps []claudeProc
	for _, l := range strings.Split(out, "\n") {
		m := psLine.FindStringSubmatch(l)
		if m == nil || !claudeCmd.MatchString(m[3]) || notClaude.MatchString(m[3]) {
			continue
		}
		pid, _ := strconv.Atoi(m[1])
		ps = append(ps, claudeProc{pid, m[2], m[3]})
	}
	return ps
}

func normStatus(s string) string {
	switch s {
	case "busy", "idle", "blocked":
		return s
	}
	return "unknown"
}

// mergeStatus overlays the Rex pane state on the sessions-file status; a blocked pane always wins.
func mergeStatus(claude, rexState string) string {
	st := normStatus(claude)
	switch {
	case rexState == "blocked":
		return "blocked"
	case st != "unknown":
		return st
	case rexState == "working":
		return "busy"
	case rexState == "idle" || rexState == "done":
		return "idle"
	}
	return st
}

type claudeEntry struct {
	Pid     int            `json:"pid"`
	Project string         `json:"project"`
	Cwd     string         `json:"cwd"`
	Started string         `json:"started"`
	Name    string         `json:"name"`
	Status  string         `json:"status"`
	Session *sessionHeader `json:"session,omitempty"`
}

type sessionHeader struct {
	Title      string `json:"title"`
	LastActive string `json:"last_active"`
}

func (e *env) listClaude(ctx context.Context, args json.RawMessage) (any, error) {
	if err := decodeArgs(args, &struct{}{}); err != nil {
		return nil, err
	}
	out, err := e.Exec(ctx, "ps", "-ax", "-o", "pid=,lstart=,command=")
	if err != nil {
		return nil, err
	}
	list := []claudeEntry{}
	for _, p := range parsePS(string(out)) {
		s := e.claudeSession(p.pid)
		cwd := s.Cwd
		if cwd == "" {
			cwd = e.cwdOf(ctx, p.pid)
		}
		if cwd == "" {
			continue // process exited or not ours to read
		}
		c := claudeEntry{Pid: p.pid, Project: filepath.Base(cwd), Cwd: cwd, Started: p.started, Name: s.Name, Status: normStatus(s.Status)}
		if files := e.transcripts(s, cwd, 1); len(files) > 0 {
			if sum, err := summarize(files[0].path, files[0].mtime); err == nil {
				c.Session = &sessionHeader{sum.Title, sum.LastActive}
			}
		}
		list = append(list, c)
	}
	return list, nil
}

type transcriptFile struct {
	path  string
	mtime time.Time
}

const recentWindow = 2 * time.Hour

// transcripts returns the exact transcript named by the sessions file when it exists (regardless of age),
// otherwise the n most recently modified transcripts in the cwd's project dir touched within recentWindow.
func (e *env) transcripts(s claudeSession, cwd string, n int) []transcriptFile {
	dir := filepath.Join(e.Home, ".claude", "projects", projectDirName(cwd))
	if s.SessionID != "" {
		p := filepath.Join(dir, s.SessionID+".jsonl")
		if fi, err := os.Stat(p); err == nil {
			return []transcriptFile{{p, fi.ModTime()}}
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var fs []transcriptFile
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".jsonl") {
			continue
		}
		fi, err := de.Info()
		if err != nil || time.Since(fi.ModTime()) > recentWindow {
			continue
		}
		fs = append(fs, transcriptFile{filepath.Join(dir, de.Name()), fi.ModTime()})
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].mtime.After(fs[j].mtime) })
	if len(fs) > n {
		fs = fs[:n]
	}
	return fs
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

func projectDirName(cwd string) string { return nonAlnum.ReplaceAllString(cwd, "-") }

// claudeSummary is one read_claude item: Asked and Reply are short and speakable; Detail (Claude's last
// two texts verbatim, oldest first) is only returned with verbose.
type claudeSummary struct {
	Title       string   `json:"title"`
	LastActive  string   `json:"last_active"`
	Status      string   `json:"status"`
	SessionName string   `json:"session_name"`
	Asked       string   `json:"asked"`
	Reply       string   `json:"reply"`
	Detail      []string `json:"detail,omitempty"`
}

var (
	reFence    = regexp.MustCompile("(?s)```.*?(```|$)")
	reDropLine = regexp.MustCompile(`(?m)^[ \t]*(\|.*|-{3,}[ \t]*)$`) // table rows, horizontal rules
	reLineMark = regexp.MustCompile(`(?m)^[ \t]*(#+|[-*+])[ \t]+`)
	reInline   = regexp.MustCompile("`[^`\n]+`")
	reURL      = regexp.MustCompile(`https?://[^\s)）\]>，。]+`)
	reHomePath = regexp.MustCompile(`~/[\w.\-/]*`)
	reAbsPath  = regexp.MustCompile(`(^|[^\w.\-/])(/[\w.\-]+){2,}/?`) // two segments up, so "/model" stays
	reSpaces   = regexp.MustCompile(`\s+`)
)

// cleanText strips what a voice should not read out: code blocks, inline code, links, paths, tables and
// markdown marks, then folds whitespace into single spaces.
func cleanText(s string) string {
	s = reFence.ReplaceAllString(s, " ")
	s = reDropLine.ReplaceAllString(s, "")
	s = reLineMark.ReplaceAllString(s, "")
	// Short names in backticks (`rex-voice`, `/model`) are words worth saying; longer snippets are not.
	s = reInline.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.Trim(m, "`")
		if len([]rune(inner)) <= 24 && !strings.ContainsAny(inner, " \t(){}=;") && strings.Count(inner, "/") <= 1 {
			return inner
		}
		return "代码"
	})
	s = reURL.ReplaceAllString(s, "链接")
	s = reHomePath.ReplaceAllString(s, "某文件")
	s = reAbsPath.ReplaceAllString(s, "${1}某文件")
	s = strings.ReplaceAll(s, "*", "")
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

const noRecent = "两小时内没有活动的会话记录"

func (e *env) readClaude(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Pid     int    `json:"pid"`
		Block   string `json:"block"`
		Verbose bool   `json:"verbose"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	rexState := ""
	switch {
	case a.Block != "":
		sid, err := e.sessionOf(ctx, a.Block)
		if err != nil {
			return nil, err
		}
		st, err := e.programStatus(ctx, sid, a.Block)
		if err != nil {
			return nil, err
		}
		if st == nil || st.Owner.Pid == 0 {
			return nil, errors.New("这个窗格里没有在跑的 Claude")
		}
		if a.Pid == 0 {
			a.Pid = st.Owner.Pid
		}
		rexState = st.State
	case a.Pid > 0:
		// Best effort: find the pane this pid runs in so a blocked prompt shows up; Claude may be outside Rex.
		if ps, err := e.panes(ctx); err == nil {
			for _, p := range ps {
				if p.pid == a.Pid {
					rexState = p.State
				}
			}
		}
	default:
		return nil, errors.New("给我 pid 或 block")
	}
	s := e.claudeSession(a.Pid)
	cwd := s.Cwd
	if cwd == "" {
		cwd = e.cwdOf(ctx, a.Pid)
	}
	if cwd == "" {
		return nil, errors.New("拿不到进程目录，这个 Claude 可能已经退出")
	}
	status := mergeStatus(s.Status, rexState)
	files := e.transcripts(s, cwd, 2)
	out := []claudeSummary{}
	for _, f := range files {
		sum, err := summarize(f.path, f.mtime)
		if err != nil {
			return nil, err
		}
		sum.Status, sum.SessionName = status, s.Name
		if !a.Verbose {
			sum.Detail = nil
		}
		out = append(out, sum)
	}
	if len(out) == 0 {
		return nil, errors.New(noRecent)
	}
	return out, nil
}

// tailChunk is a var so tests can force lines to straddle chunk boundaries.
var tailChunk int64 = 64 << 10

type transcriptRow struct {
	Type    string `json:"type"`
	IsMeta  bool   `json:"isMeta"`
	AiTitle string `json:"aiTitle"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func rowText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct{ Type, Text string }
	if json.Unmarshal(content, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

// summarize reads the transcript backwards in chunks and stops once it has the last title,
// the last user instruction and two assistant texts, so multi-MB transcripts are not read whole.
func summarize(path string, mtime time.Time) (claudeSummary, error) {
	sum := claudeSummary{LastActive: mtime.Format(time.RFC3339), Detail: []string{}}
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return sum, err
	}
	var asst []string // newest first, each cut to 1500 runes
	lastReply := ""   // newest assistant text uncut, so cleanText sees whole code fences
	lastUser := ""
	firstUser := "" // earliest seen so far; title fallback when the file has no aiTitle
	done := func() bool { return sum.Title != "" && lastUser != "" && len(asst) >= 2 }
	process := func(line []byte) {
		var r transcriptRow
		if len(bytes.TrimSpace(line)) == 0 || json.Unmarshal(line, &r) != nil {
			return
		}
		if r.AiTitle != "" && sum.Title == "" {
			sum.Title = r.AiTitle
		}
		switch r.Type {
		case "user":
			if t := rowText(r.Message.Content); !r.IsMeta && t != "" && !strings.HasPrefix(t, "<") {
				if lastUser == "" {
					lastUser = t
				}
				firstUser = t
			}
		case "assistant":
			if t := rowText(r.Message.Content); t != "" && len(asst) < 2 {
				if len(asst) == 0 {
					lastReply = t
				}
				asst = append(asst, truncRunes(t, 1500))
			}
		}
	}
	pos, carry := fi.Size(), []byte(nil)
	for pos > 0 && !done() {
		n := min(tailChunk, pos)
		pos -= n
		buf := make([]byte, n, n+int64(len(carry)))
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return sum, err
		}
		lines := bytes.Split(append(buf, carry...), []byte("\n"))
		carry = nil
		if pos > 0 { // first piece may be the tail of a line that starts in an earlier chunk
			carry, lines = lines[0], lines[1:]
		}
		for i := len(lines) - 1; i >= 0 && !done(); i-- {
			process(lines[i])
		}
	}
	if sum.Title == "" {
		sum.Title = truncRunes(firstUser, 60)
	}
	sum.Asked = truncRunes(strings.Join(strings.Fields(lastUser), " "), 80)
	sum.Reply = truncRunes(cleanText(lastReply), 400)
	for i := len(asst) - 1; i >= 0; i-- {
		sum.Detail = append(sum.Detail, asst[i])
	}
	return sum, nil
}
