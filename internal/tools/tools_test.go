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
)

type fakeExec struct {
	mu    sync.Mutex
	resp  map[string]string // command line -> testdata file
	fail  map[string]string // command line -> stderr
	calls []string
}

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
	if strings.HasPrefix(cmd, "rex send") {
		return nil, nil
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
	for _, tl := range NewRexTools(Options{Exec: f.exec, Home: fixtureHome(t), Audit: audit}) {
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
	for _, tl := range NewRexTools(Options{Exec: f.exec, Home: h}) {
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
	for _, text := range forbiddenExamples {
		args, _ := json.Marshal(map[string]string{"block": bVoice, "text": text})
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
		if json.Unmarshal([]byte(l), &a) != nil || a["result"] != "refused" || a["text"] != forbiddenExamples[i] || a["block"] != bVoice || a["tool"] != "send_to_pane" || a["time"] == "" {
			t.Errorf("audit line %d: %s", i, l)
		}
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
