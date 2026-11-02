package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fixture ids, recorded from this Mac on 2026-11-01 (see testdata).
const (
	sDev     = "session:01a19207-2591-7b3b-9840-ae5ae8cd1da6"
	sJobs    = "session:01a19764-f868-799c-87c5-22b8d46ab962"
	bGeoUS   = "block:01a1926c-0d0e-7971-951b-d8b231bdd009" // pid 420, has sessions json
	bGeoUSCF = "block:01a19632-9cf6-70cf-be07-cf46bee0ad5f" // pid 41052, no sessions json -> lsof
	bVoice   = "block:01a19a0e-06a0-70cb-8e2c-311f5704d684" // pid 26673, has sessions json
	voiceSID = "2267b08d-dd9a-4d76-b08a-c20417adfd9e"

	bShell   = "block:01a19b95-c319-76ed-8bd6-ab787c78ec9b" // plain shell, no program_status records (2026-11-02)
	wGeoUS   = "window:01a1926c-0d38-7b00-9220-4256b9b2b00d"
	newBlock = "block:01a19b96-5ef9-7065-b31f-b304826241c8" // from testdata/rex_run.json
)

// withShell puts the plain shell pane bShell into the jobs session.
func withShell(f *fakeExec) {
	f.resp["rex block ls -s "+sJobs+" --json"] = "block_ls_shell.json"
	f.resp["rex block call -s "+sJobs+" -b "+bShell+" program_status"] = "program_status_empty.json"
}

// cmds returns the fake's command lines starting with prefix.
func (f *fakeExec) cmds(prefix string) []string {
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

type fakeExec struct {
	mu    sync.Mutex
	resp  map[string]string // command line -> testdata file
	fail  map[string]string // command line -> stderr
	calls []string
}

// alive stands in for pidAlive: fixture pids are not real processes.
var alive = func(int) bool { return true }

func newFake() *fakeExec {
	ps := func(b string) string { return "rex block call -s " + sDev + " -b " + b + " program_status" }
	return &fakeExec{
		resp: map[string]string{
			"rex ls --json":                        "rex_ls.json",
			"rex block ls -s " + sDev + " --json":  "block_ls_rex-voice-dev.json",
			"rex block ls -s " + sJobs + " --json": "block_ls_jobs.json",
			ps(bGeoUS):                             "program_status_01a1926c-0d0e-7971-951b-d8b231bdd009.json",
			ps(bGeoUSCF):                           "program_status_01a19632-9cf6-70cf-be07-cf46bee0ad5f.json",
			ps(bVoice):                             "program_status_01a19a0e_msg.json",
			"rex capture -s " + sDev + " -b " + bGeoUS: "capture_01a1926c.txt",
			"ps -ax -o pid=,lstart=,command=":          "ps.txt",
			"lsof -a -p 41052 -d cwd -Fn":              "lsof_41052.txt",
		},
		fail: map[string]string{},
	}
}

func (f *fakeExec) exec(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	f.mu.Unlock()
	if msg, ok := f.fail[cmd]; ok {
		return nil, errors.New(msg)
	}
	// Commands that print nothing on success.
	for _, p := range []string{"rex send", "rex block close ", "rex block rename ", "rex window ", "rex session "} {
		if strings.HasPrefix(cmd, p) {
			return nil, nil
		}
	}
	file, ok := f.resp[cmd]
	if !ok {
		return nil, errors.New("fake: unexpected command " + cmd)
	}
	return os.ReadFile(filepath.Join("testdata", file))
}

// fixtureHome lays out ~/.claude/sessions (pids 420, 26673) and the exact transcript for pid 26673.
func fixtureHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, pid := range []string{"420", "26673"} {
		copyFile(t, filepath.Join("testdata", "sessions", pid+".json"), filepath.Join(home, ".claude", "sessions", pid+".json"))
	}
	copyFile(t, "testdata/transcript.jsonl", filepath.Join(home, ".claude", "projects", "-Users-andy-data-claude-rex-voice", voiceSID+".jsonl"))
	return home
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) (*Registry, *fakeExec, *bytes.Buffer) {
	t.Helper()
	f, audit := newFake(), &bytes.Buffer{}
	r := &Registry{}
	for _, tl := range NewRexTools(Options{Alive: alive, Exec: f.exec, Home: fixtureHome(t), Audit: audit}) {
		r.Register(tl)
	}
	return r, f, audit
}

func call(t *testing.T, r *Registry, name, args string, v any) string {
	t.Helper()
	out := r.Call(context.Background(), name, args)
	if v != nil {
		if err := json.Unmarshal([]byte(out), v); err != nil {
			t.Fatalf("%s(%s) = %s: %v", name, args, out, err)
		}
	}
	return out
}

func wantError(t *testing.T, out, substr string) {
	t.Helper()
	var e struct{ Error string }
	if json.Unmarshal([]byte(out), &e) != nil || !strings.Contains(e.Error, substr) {
		t.Fatalf("got %s, want error containing %q", out, substr)
	}
}

func TestListPanes(t *testing.T) {
	r, f, _ := setup(t)
	var ps []pane
	call(t, r, "list_panes", "", &ps)
	if len(ps) != 3 {
		t.Fatalf("%d panes: %+v", len(ps), ps)
	}
	want := map[string]pane{
		bGeoUS:   {Session: "rex-voice-dev", Label: "shell", Project: "geo-us", App: "claude-code", State: "done"},
		bGeoUSCF: {Session: "rex-voice-dev", Label: "shell", Project: "geo-us", App: "claude-code", State: "done"},
		bVoice:   {Session: "rex-voice-dev", Label: "rex-voice", Project: "rex-voice", App: "claude-code", State: "working"},
	}
	for _, p := range ps {
		w := want[p.Block]
		w.Block = p.Block
		if p.Block == bVoice {
			w.Doing = "Fetching OpenAI realtime type definitions for delete, truncating, audio formats, turn detection, server events"
		}
		if p != w {
			t.Errorf("pane %s = %+v, want %+v", p.Block, p, w)
		}
	}
	// project for 420 / 26673 must come from sessions json: no lsof call for them.
	for _, c := range f.calls {
		if strings.HasPrefix(c, "lsof") && !strings.Contains(c, "41052") {
			t.Errorf("unexpected %s", c)
		}
	}
}

func TestReadPane(t *testing.T) {
	r, _, _ := setup(t)
	var screen string
	call(t, r, "read_pane", `{"block":"`+bGeoUS+`"}`, &screen)
	lines := strings.Split(screen, "\n")
	if lines[0] == "" || lines[len(lines)-1] == "" {
		t.Errorf("leading/trailing empty line kept")
	}
	for i, l := range lines {
		if strings.TrimRight(l, " \t") != l {
			t.Errorf("line %d has trailing blanks: %q", i, l)
		}
		if i > 0 && l == "" && lines[i-1] == "" {
			t.Errorf("lines %d-%d: consecutive empty lines", i-1, i)
		}
	}
	call(t, r, "read_pane", `{"block":"`+bGeoUS+`","lines":2}`, &screen)
	if got := strings.Split(screen, "\n"); len(got) != 2 || got[1] != "  ⏵⏵ bypass permissions on (shift+tab to cycle)" {
		t.Errorf("lines=2 → %q", got)
	}

	wantError(t, call(t, r, "read_pane", `{"block":"block:nope"}`, nil), "没有这个窗格: block:nope")
}

func TestTailScreenCaps(t *testing.T) {
	long := strings.Repeat("字", 5000) + "\n" + strings.Repeat("b", 5000) + "\n"
	got := tailScreen(long, 40)
	if n := len([]rune(got)); n != maxPaneRunes || !strings.HasSuffix(got, "bbb") {
		t.Errorf("len %d, suffix %q", n, got[len(got)-3:])
	}
	if got := tailScreen("a  \n\n\n \nb\t\n\n", 40); got != "a\n\nb" {
		t.Errorf("fold = %q", got)
	}
}

func TestRexFailureReturnsStderr(t *testing.T) {
	r, f, _ := setup(t)
	stderr, _ := os.ReadFile("testdata/rex_error_stderr.txt")
	f.fail["rex capture -s "+sDev+" -b "+bGeoUS] = string(stderr) + strings.Repeat("x", 400)
	out := call(t, r, "read_pane", `{"block":"`+bGeoUS+`"}`, nil)
	wantError(t, out, `Error: block target "block:00000000`)
	var e struct{ Error string }
	json.Unmarshal([]byte(out), &e)
	if n := len([]rune(e.Error)); n != 300 {
		t.Errorf("error is %d chars, want 300", n)
	}
}

func TestListClaude(t *testing.T) {
	r, _, _ := setup(t)
	var cs []claudeEntry
	call(t, r, "list_claude", "", &cs)
	if len(cs) != 3 {
		t.Fatalf("got %+v", cs)
	}
	byPid := map[int]claudeEntry{}
	for _, c := range cs {
		byPid[c.Pid] = c
	}
	v := byPid[26673]
	if v.Project != "rex-voice" || v.Name != "rex-voice-b4" || v.Status != "busy" || v.Started != "Sun Nov  1 18:19:32 2026" ||
		v.Session == nil || v.Session.Title != "rex-voice-ios-app" {
		t.Errorf("26673 = %+v %+v", v, v.Session)
	}
	if g := byPid[41052]; g.Cwd != "/Users/andy/data/claude/geo-us" || g.Status != "unknown" || g.Session != nil {
		t.Errorf("41052 (lsof fallback) = %+v", g)
	}
	if g := byPid[420]; g.Name != "geo us" || g.Status != "idle" {
		t.Errorf("420 = %+v", g)
	}
}

func TestParsePSFilter(t *testing.T) {
	out := `  101 Sun Nov  1 18:19:32 2026     claude --dangerously-skip-permissions
  102 Sun Nov  1 18:19:32 2026     /opt/homebrew/bin/claude
  103 Sun Nov  1 18:19:32 2026     claude-code-helper
  104 Sun Nov  1 18:19:32 2026     /Applications/Claude.app/Contents/MacOS/claude
  105 Sun Nov  1 18:19:32 2026     /Users/andy/data/claude/geo/.venv/bin/python3 x`
	var pids []int
	for _, p := range parsePS(out) {
		pids = append(pids, p.pid)
	}
	if len(pids) != 2 || pids[0] != 101 || pids[1] != 102 {
		t.Errorf("pids %v, want [101 102]", pids)
	}
}

func checkVoiceSummary(t *testing.T, s claudeSummary) {
	t.Helper()
	if s.Title != "rex-voice-ios-app" {
		t.Errorf("title %q, want last aiTitle", s.Title)
	}
	// The isMeta skill line and the <command-name>/<local-command-stdout> lines come after this one.
	if !strings.HasPrefix(s.LastUser, "我最近在想一件事情") {
		t.Errorf("last_user %q", s.LastUser)
	}
	if len(s.ClaudeRecent) != 2 || !strings.HasPrefix(s.ClaudeRecent[0], "可行，但 iMessage") || !strings.HasPrefix(s.ClaudeRecent[1], "新会话已经在") {
		t.Errorf("claude_recent %q", s.ClaudeRecent)
	}
}

func TestReadClaudeExact(t *testing.T) {
	r, _, _ := setup(t)
	for _, args := range []string{`{"pid":26673}`, `{"block":"` + bVoice + `"}`} {
		var got []claudeSummary
		call(t, r, "read_claude", args, &got)
		if len(got) != 1 {
			t.Fatalf("%s: %d summaries", args, len(got))
		}
		checkVoiceSummary(t, got[0])
		if got[0].Status != "busy" || got[0].SessionName != "rex-voice-b4" {
			t.Errorf("%s: status %q name %q", args, got[0].Status, got[0].SessionName)
		}
	}
}

func TestReadClaudeFallbackByMtime(t *testing.T) {
	f := newFake()
	h := t.TempDir()
	dir := filepath.Join(h, ".claude", "projects", "-Users-andy-data-claude-geo-us")
	now := time.Now()
	for name, age := range map[string]time.Duration{"old.jsonl": 3 * time.Hour, "mid.jsonl": 10 * time.Minute, "new.jsonl": time.Minute, "notes.txt": 0} {
		p := filepath.Join(dir, name)
		copyFile(t, "testdata/transcript.jsonl", p)
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	reg := &Registry{}
	for _, tl := range NewRexTools(Options{Alive: alive, Exec: f.exec, Home: h}) {
		reg.Register(tl)
	}
	var got []claudeSummary
	call(t, reg, "read_claude", `{"block":"`+bGeoUSCF+`"}`, &got) // pid 41052: no sessions json
	if len(got) != 2 {
		t.Fatalf("%d summaries, want 2 (old one excluded)", len(got))
	}
	if !(got[0].LastActive > got[1].LastActive) {
		t.Errorf("not newest first: %s, %s", got[0].LastActive, got[1].LastActive)
	}
	for _, s := range got {
		checkVoiceSummary(t, s)
		if s.Status != "idle" { // no sessions json; Rex says done
			t.Errorf("status %q", s.Status)
		}
	}
}

func TestReadClaudeNoTranscript(t *testing.T) {
	r, _, _ := setup(t) // pid 420's project dir does not exist in the fixture home
	wantError(t, call(t, r, "read_claude", `{"pid":420}`, nil), noRecent)
	wantError(t, call(t, r, "read_claude", `{}`, nil), "pid 或 block")
}

func TestMergeStatus(t *testing.T) {
	for _, c := range [][3]string{
		{"busy", "blocked", "blocked"}, {"idle", "working", "idle"}, {"", "working", "busy"},
		{"", "done", "idle"}, {"weird", "", "unknown"}, {"busy", "", "busy"},
	} {
		if got := mergeStatus(c[0], c[1]); got != c[2] {
			t.Errorf("mergeStatus(%q,%q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestSummarizeChunkBoundaries(t *testing.T) {
	p := "testdata/transcript.jsonl"
	whole, err := summarize(p, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer func(old int64) { tailChunk = old }(tailChunk)
	for _, n := range []int64{1, 7, 100, 4096} {
		tailChunk = n
		got, err := summarize(p, time.Unix(0, 0))
		if err != nil {
			t.Fatal(err)
		}
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(whole)
		if !bytes.Equal(g, w) {
			t.Errorf("chunk %d: %s\nwant %s", n, g, w)
		}
	}
	checkVoiceSummary(t, whole)
}

var forbiddenExamples = []string{
	"去生产看一下", "部署 geo", "可以上线了", "发布新版", "回滚到上一版", "跑一下迁移",
	"DEPLOY it", "rm -rf build", "git push origin main", "sudo reboot",
}

func TestSendToPaneRefused(t *testing.T) {
	r, f, audit := setup(t)
	withShell(f)
	for _, text := range forbiddenExamples {
		args, _ := json.Marshal(map[string]string{"block": bShell, "text": text})
		var got map[string]string
		call(t, r, "send_to_pane", string(args), &got)
		if got["refused"] != refusedMsg {
			t.Errorf("%q not refused: %v", text, got)
		}
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "rex send") {
			t.Errorf("sent despite refusal: %s", c)
		}
	}
	lines := strings.Split(strings.TrimSpace(audit.String()), "\n")
	if len(lines) != len(forbiddenExamples) {
		t.Fatalf("%d audit lines, want %d", len(lines), len(forbiddenExamples))
	}
	for i, l := range lines {
		var a map[string]string
		if json.Unmarshal([]byte(l), &a) != nil || a["result"] != "refused" || a["text"] != forbiddenExamples[i] || a["block"] != bShell || a["tool"] != "send_to_pane" || a["time"] == "" {
			t.Errorf("audit line %d: %s", i, l)
		}
	}
}

// Claude panes skip the forbidden-word gate (built-in and ForbidExtra); shell panes get both.
func TestSendToPaneClaudeVsShell(t *testing.T) {
	f := newFake()
	withShell(f)
	r := &Registry{}
	for _, tl := range NewRexTools(Options{Alive: alive, Exec: f.exec, Home: t.TempDir(), ForbidExtra: []string{"DROP TABLE"}}) {
		r.Register(tl)
	}
	for _, text := range []string{"部署 geo 到生产", "please drop table users"} {
		args, _ := json.Marshal(map[string]string{"block": bVoice, "text": text})
		var got map[string]string
		if call(t, r, "send_to_pane", string(args), &got); got["sent"] != text {
			t.Errorf("claude pane %q: %v", text, got)
		}
		args, _ = json.Marshal(map[string]string{"block": bShell, "text": text})
		got = nil
		if call(t, r, "send_to_pane", string(args), &got); got["refused"] != refusedMsg {
			t.Errorf("shell pane %q: %v", text, got)
		}
	}
	if s := f.cmds("rex send -s " + sJobs); len(s) != 0 {
		t.Errorf("sent to shell: %v", s)
	}
	if s := f.cmds("rex send -s " + sDev); len(s) != 2 {
		t.Errorf("claude sends: %v", s)
	}
}

func setupWorkspace(t *testing.T) (*Registry, *fakeExec, *bytes.Buffer, string) {
	t.Helper()
	f, audit := newFake(), &bytes.Buffer{}
	home, _ := filepath.EvalSymlinks(t.TempDir()) // open_pane reports real paths (/var -> /private/var on macOS)
	if err := os.MkdirAll(filepath.Join(home, "data", "claude", "geo"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, "data", "claude", "notes.txt"), nil, 0o644)
	r := &Registry{}
	for _, tl := range NewRexTools(Options{Alive: alive, Exec: f.exec, Home: home, Audit: audit}) {
		r.Register(tl)
	}
	return r, f, audit, home
}

func TestOpenPane(t *testing.T) {
	r, f, _, home := setupWorkspace(t)
	geo := filepath.Join(home, "data", "claude", "geo")
	claudeCmd := "rex run -s geo us -c " + geo + " --label fix-login --focus=false --json -- claude --dangerously-skip-permissions -n fix-login"
	shellCmd := "rex run -s " + sDev + " -c " + geo + " --label geo --focus=false --json --keep-open -- zsh -l"
	f.resp[claudeCmd], f.resp[shellCmd] = "rex_run.json", "rex_run.json"

	var got map[string]string
	call(t, r, "open_pane", `{"session":"geo us","project":"geo","name":"fix-login"}`, &got)
	if got["block"] != newBlock || got["session"] != "geo us" {
		t.Errorf("claude: %v", got)
	}
	call(t, r, "open_pane", `{"session":"`+sDev+`","project":"`+geo+`","kind":"shell"}`, &got)
	if got["block"] != newBlock {
		t.Errorf("shell: %v", got)
	}
	if runs := f.cmds("rex run"); strings.Join(runs, "\n") != claudeCmd+"\n"+shellCmd {
		t.Errorf("commands:\n%s", strings.Join(runs, "\n"))
	}
}

func TestOpenPaneRejected(t *testing.T) {
	r, f, _, home := setupWorkspace(t)
	wantError(t, call(t, r, "open_pane", `{"session":"x","project":"nope"}`, nil), "不是目录")
	wantError(t, call(t, r, "open_pane", `{"session":"x","project":"notes.txt"}`, nil), "不是目录")
	wantError(t, call(t, r, "open_pane", `{"session":"x","project":"../../.."}`, nil), "不在")
	wantError(t, call(t, r, "open_pane", `{"session":"x","project":"/etc"}`, nil), "不在")
	wantError(t, call(t, r, "open_pane", `{"session":"x","project":"geo","kind":"vim"}`, nil), "kind")
	f.fail["rex run -s nosuch -c "+filepath.Join(home, "data", "claude", "geo")+" --label geo --focus=false --json -- claude --dangerously-skip-permissions"] = "session target \"nosuch\" not found"
	wantError(t, call(t, r, "open_pane", `{"session":"nosuch","project":"geo"}`, nil), "not found")
	if runs := f.cmds("rex run"); len(runs) != 1 {
		t.Errorf("rex run called for invalid input: %v", runs)
	}
}

func TestClosePane(t *testing.T) {
	r, f, audit := setup(t)
	var got map[string]string
	call(t, r, "close_pane", `{"block":"`+bGeoUS+`"}`, &got) // done: closes
	if got["closed"] != bGeoUS {
		t.Errorf("pane: %v", got)
	}
	call(t, r, "close_pane", `{"block":"`+bGeoUS+`","scope":"window"}`, &got) // bVoice is in another window
	if got["closed"] != wGeoUS {
		t.Errorf("window: %v", got)
	}
	call(t, r, "close_pane", `{"block":"`+bGeoUS+`","scope":"session","force":true}`, &got)
	if got["closed"] != sDev {
		t.Errorf("session force: %v", got)
	}
	want := []string{"rex block close -s " + sDev + " " + bGeoUS, "rex window close -s " + sDev + " " + wGeoUS, "rex session kill " + sDev}
	if c := append(f.cmds("rex block close"), append(f.cmds("rex window"), f.cmds("rex session")...)...); strings.Join(c, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s", strings.Join(c, "\n"))
	}
	if n := strings.Count(audit.String(), `"tool":"close_pane"`); n != 3 || !strings.Contains(audit.String(), `"result":"closed"`) {
		t.Errorf("audit %s", audit.String())
	}
}

func TestClosePaneRefusedWhileWorking(t *testing.T) {
	r, f, audit := setup(t)
	var got map[string]string
	call(t, r, "close_pane", `{"block":"`+bVoice+`"}`, &got)
	if !strings.HasPrefix(got["refused"], bVoice+" 正在 Fetching OpenAI") || !strings.HasSuffix(got["refused"], "要关请确认 force") {
		t.Errorf("pane: %v", got)
	}
	got = nil
	call(t, r, "close_pane", `{"block":"`+bGeoUS+`","scope":"session"}`, &got) // bGeoUS is done but bVoice works
	if !strings.HasPrefix(got["refused"], bVoice) {
		t.Errorf("session: %v", got)
	}
	wantError(t, call(t, r, "close_pane", `{"block":"`+bGeoUS+`","scope":"tab"}`, nil), "scope")
	if c := append(f.cmds("rex block close"), f.cmds("rex session")...); len(c) != 0 {
		t.Errorf("closed despite refusal: %v", c)
	}
	if !strings.Contains(audit.String(), `"result":"refused"`) {
		t.Errorf("audit %s", audit.String())
	}
}

func TestRename(t *testing.T) {
	r, f, _ := setup(t)
	for _, a := range []string{
		`{"target":"pane","id":"` + bVoice + `","name":"voice"}`,
		`{"target":"window","id":"` + bGeoUS + `","name":"-geo"}`,
		`{"target":"session","id":"rex-voice-dev","name":" dev "}`,
	} {
		var got map[string]string
		if call(t, r, "rename", a, &got); got["renamed"] == "" {
			t.Errorf("%s: %v", a, got)
		}
	}
	want := []string{
		"rex block rename -s " + sDev + " -- " + bVoice + " voice",
		"rex window rename -s " + sDev + " -- " + wGeoUS + " -geo",
		"rex session rename -- rex-voice-dev dev",
	}
	if c := append(f.cmds("rex block rename"), append(f.cmds("rex window"), f.cmds("rex session")...)...); strings.Join(c, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s", strings.Join(c, "\n"))
	}
}

func TestRenameRejected(t *testing.T) {
	r, f, _ := setup(t)
	wantError(t, call(t, r, "rename", `{"target":"pane","id":"`+bVoice+`","name":"  "}`, nil), "name 是空的")
	wantError(t, call(t, r, "rename", `{"target":"tab","id":"`+bVoice+`","name":"x"}`, nil), "target")
	wantError(t, call(t, r, "rename", `{"target":"pane","id":"block:nope","name":"x"}`, nil), "没有这个窗格")
	if c := append(f.cmds("rex block rename"), f.cmds("rex session")...); len(c) != 0 {
		t.Errorf("renamed: %v", c)
	}
}

func TestSendKeys(t *testing.T) {
	r, f, audit := setup(t)
	var got map[string]string
	call(t, r, "send_keys", `{"block":"`+bVoice+`","keys":["Down","enter","2","shift+tab"]}`, &got)
	if got["sent"] != "down enter 2 shift+tab" {
		t.Errorf("got %v", got)
	}
	if c := f.cmds("rex send-key"); len(c) != 1 || c[0] != "rex send-key -s "+sDev+" -b "+bVoice+" down enter 2 shift+tab" {
		t.Errorf("commands %v", c)
	}
	var a map[string]string
	if json.Unmarshal(audit.Bytes(), &a) != nil || a["tool"] != "send_keys" || a["text"] != "down enter 2 shift+tab" || a["result"] != "sent" {
		t.Errorf("audit %s", audit.String())
	}
}

func TestSendKeysRejected(t *testing.T) {
	r, f, _ := setup(t)
	wantError(t, call(t, r, "send_keys", `{"block":"`+bVoice+`","keys":["down","y"]}`, nil), `不支持的键 "y"`)
	wantError(t, call(t, r, "send_keys", `{"block":"`+bVoice+`","keys":["ctrl+d"]}`, nil), "不支持的键")
	wantError(t, call(t, r, "send_keys", `{"block":"`+bVoice+`","keys":[]}`, nil), "keys 是空的")
	if c := f.cmds("rex send-key"); len(c) != 0 {
		t.Errorf("sent despite rejection: %v", c)
	}
}

func TestSendToPaneSent(t *testing.T) {
	r, f, audit := setup(t)
	var got map[string]string
	call(t, r, "send_to_pane", `{"block":"`+bVoice+`","text":"-v 跑一下 go test"}`, &got)
	if got["sent"] != "-v 跑一下 go test" {
		t.Fatalf("got %v", got)
	}
	var sends []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "rex send") {
			sends = append(sends, c)
		}
	}
	want := []string{
		"rex send -s " + sDev + " -b " + bVoice + " -- -v 跑一下 go test",
		"rex send-key -s " + sDev + " -b " + bVoice + " enter",
	}
	if strings.Join(sends, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(sends, "\n"), strings.Join(want, "\n"))
	}
	var a map[string]string
	if json.Unmarshal(audit.Bytes(), &a) != nil || a["result"] != "sent" || a["text"] != "-v 跑一下 go test" {
		t.Errorf("audit %s", audit.String())
	}
}

func TestRegistryCall(t *testing.T) {
	r := &Registry{}
	var reason string
	r.Register(HangUpDef(func(s string) { reason = s }))
	r.Register(Tool{Name: "boom", Parameters: noParams, Run: func(context.Context, json.RawMessage) (any, error) { panic("kaboom") }})
	ctx := context.Background()

	wantError(t, r.Call(ctx, "nope", "{}"), "没有这个工具: nope")
	wantError(t, r.Call(ctx, "hang_up", "{"), "合法的 JSON")
	wantError(t, r.Call(ctx, "hang_up", "[]"), "合法的 JSON")
	wantError(t, r.Call(ctx, "hang_up", `{"why":"x"}`), "参数不合法")
	wantError(t, r.Call(ctx, "boom", ""), "kaboom")

	if out := r.Call(ctx, "hang_up", `{"reason":"user"}`); out != `{"ok":true}` || reason != "user" {
		t.Errorf("hang_up = %s, reason %q", out, reason)
	}
	defs, _ := json.Marshal(r.Defs())
	if !strings.HasPrefix(string(defs), `[{"type":"function","name":"hang_up","description":"挂断`) {
		t.Errorf("defs %s", defs)
	}
}

// A pane whose Claude has exited keeps app=claude-code in Rex; it is a shell again and must be gated.
func TestSendToPaneDeadClaudeIsShell(t *testing.T) {
	f, audit := newFake(), &bytes.Buffer{}
	r := &Registry{}
	for _, tl := range NewRexTools(Options{Alive: func(int) bool { return false }, Exec: f.exec, Home: fixtureHome(t), Audit: audit}) {
		r.Register(tl)
	}
	var got map[string]string
	call(t, r, "send_to_pane", `{"block":"`+bVoice+`","text":"部署 geo 到生产"}`, &got)
	if got["refused"] != refusedMsg || len(f.cmds("rex send")) != 0 {
		t.Errorf("dead claude pane: %v %v", got, f.cmds("rex send"))
	}
	got = nil
	call(t, r, "send_to_pane", `{"block":"`+bVoice+`","text":"ls"}`, &got) // harmless text still goes through
	if got["sent"] != "ls" {
		t.Errorf("plain text: %v", got)
	}
}

func TestSendKeysShellOnlyInterrupt(t *testing.T) {
	r, f, _ := setup(t)
	withShell(f)
	wantError(t, call(t, r, "send_keys", `{"block":"`+bShell+`","keys":["enter"]}`, nil), "不是 Claude 窗格")
	if c := f.cmds("rex send-key"); len(c) != 0 {
		t.Errorf("sent to shell: %v", c)
	}
	var got map[string]string
	call(t, r, "send_keys", `{"block":"`+bShell+`","keys":["ctrl+c"]}`, &got)
	if got["sent"] != "ctrl+c" || len(f.cmds("rex send-key -s "+sJobs+" -b "+bShell+" ctrl+c")) != 1 {
		t.Errorf("ctrl+c: %v %v", got, f.cmds("rex send-key"))
	}
}

func TestClosePaneRefusedWhenStatusFails(t *testing.T) {
	r, f, _ := setup(t)
	f.fail["rex block call -s "+sDev+" -b "+bGeoUS+" program_status"] = "rex: timeout"
	var got map[string]string
	call(t, r, "close_pane", `{"block":"`+bGeoUS+`"}`, &got)
	if !strings.Contains(got["refused"], "查不到状态") || len(f.cmds("rex block close")) != 0 {
		t.Errorf("got %v, closes %v", got, f.cmds("rex block close"))
	}
}

func TestOpenPaneSymlinks(t *testing.T) {
	r, f, _, home := setupWorkspace(t)
	geo := filepath.Join(home, "data", "claude", "geo")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "data", "claude", "evil")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "data"), filepath.Join(home, "datalink")); err != nil {
		t.Fatal(err)
	}
	wantError(t, call(t, r, "open_pane", `{"session":"`+sDev+`","project":"evil"}`, nil), "不在")
	// A link that stays inside Home resolves to the real directory (like /data -> ~/data on this Mac).
	real, _ := filepath.EvalSymlinks(geo)
	cmd := "rex run -s " + sDev + " -c " + real + " --label geo --focus=false --json -- claude --dangerously-skip-permissions"
	f.resp[cmd] = "rex_run.json"
	var got map[string]string
	call(t, r, "open_pane", `{"session":"`+sDev+`","project":"`+filepath.Join(home, "datalink", "claude", "geo")+`"}`, &got)
	if got["block"] != newBlock {
		t.Errorf("via link: %v, runs %v", got, f.cmds("rex run"))
	}
}
