package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const twoSentences = "新会话起来了，开场指令已经发了。不用你做什么。"

// fakeSummary is the DashScope compatible-mode endpoint: it answers with status and counts requests.
func fakeSummary(t *testing.T, status int) (*atomic.Int32, *[]map[string]any) {
	t.Helper()
	var n atomic.Int32
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		var b map[string]any
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &b)
		bodies = append(bodies, b)
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"`+twoSentences+`"}}],"usage":{"prompt_tokens":300,"completion_tokens":30}}`)
		} else {
			io.WriteString(w, `{"error":{"message":"boom"}}`)
		}
	}))
	t.Cleanup(srv.Close)
	old := summaryURL
	summaryURL = srv.URL
	t.Cleanup(func() { summaryURL = old })
	return &n, &bodies
}

func setupSummary(t *testing.T, logs, usage *bytes.Buffer) *Registry {
	t.Helper()
	f := newFake()
	r := &Registry{}
	sum := NewSummarizer(func() string { return "sk-test" }, SummaryModel, usage, slog.New(slog.NewTextHandler(logs, nil)))
	for _, tl := range NewRexTools(Options{Alive: alive, Exec: f.exec, Home: fixtureHome(t), Audit: io.Discard, Summarize: sum}) {
		r.Register(tl)
	}
	return r
}

func TestReadClaudeSummarized(t *testing.T) {
	n, bodies := fakeSummary(t, http.StatusOK)
	var logs, usage bytes.Buffer
	r := setupSummary(t, &logs, &usage)
	var got []claudeSummary
	call(t, r, "read_claude", `{"pid":26673}`, &got)
	if len(got) != 1 || got[0].Reply != twoSentences || n.Load() != 1 {
		t.Fatalf("read_claude = %+v (%d requests)", got, n.Load())
	}
	b := (*bodies)[0]
	msgs, _ := b["messages"].([]any)
	if b["model"] != "qwen-flash" || b["max_tokens"] != 120.0 || b["temperature"] != 0.2 || len(msgs) != 2 {
		t.Fatalf("request = %v", b)
	}
	if user := msgs[1].(map[string]any)["content"].(string); user != "新会话已经在 rex-voice 目录里起来了，正在给它发开场指令。" {
		t.Errorf("user content = %q, want the cleaned reply", user)
	}
	var l summaryUsageLine
	if err := json.Unmarshal(usage.Bytes(), &l); err != nil || strings.Count(usage.String(), "\n") != 1 {
		t.Fatalf("usage %q: %v", usage.String(), err)
	}
	if l.Kind != "summary" || l.Model != "qwen-flash" || l.Input != 300 || l.Output != 30 || l.Unpriced ||
		l.USD != (300*0.05+30*0.4)/1e6 {
		t.Errorf("usage line = %+v", l)
	}
}

// The same transcript (path + mtime) is summarized once.
func TestReadClaudeSummaryCached(t *testing.T) {
	n, _ := fakeSummary(t, http.StatusOK)
	var logs, usage bytes.Buffer
	r := setupSummary(t, &logs, &usage)
	for range 2 {
		var got []claudeSummary
		call(t, r, "read_claude", `{"pid":26673}`, &got)
		if len(got) != 1 || got[0].Reply != twoSentences {
			t.Fatalf("read_claude = %+v", got)
		}
	}
	if n.Load() != 1 {
		t.Errorf("%d summary requests, want 1", n.Load())
	}
}

func TestReadClaudeSummaryFallback(t *testing.T) {
	n, _ := fakeSummary(t, http.StatusInternalServerError)
	var logs, usage bytes.Buffer
	r := setupSummary(t, &logs, &usage)
	var got []claudeSummary
	call(t, r, "read_claude", `{"pid":26673}`, &got)
	if len(got) != 1 || got[0].Reply != "新会话已经在 rex-voice 目录里起来了，正在给它发开场指令。" {
		t.Fatalf("read_claude = %+v, want the cut reply", got)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "summary fallback") || !strings.Contains(logs.String(), "HTTP 500") {
		t.Errorf("logs = %q", logs.String())
	}
	if strings.Contains(logs.String(), "sk-test") {
		t.Error("key in the log")
	}
	if usage.Len() != 0 || n.Load() != 1 {
		t.Errorf("usage %q, requests %d", usage.String(), n.Load())
	}
}
