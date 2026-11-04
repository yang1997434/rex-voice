package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// summaryURL is DashScope's international OpenAI-compatible endpoint; tests point it at httptest.
var summaryURL = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions"

const (
	SummaryModel   = "qwen-flash"
	summaryTimeout = 4 * time.Second
	summaryPrompt  = "把下面这条 Claude 的回复压成两句口语给人在电话里听：第一句结论，第二句只在有卡住或要用户做的事时说。不念命令、路径、代码、链接、编号。"
	summaryInput   = 4000 // runes of cleaned reply sent to the model
)

// summaryPrices is USD per 1M tokens, international site, input ≤ 256K tier
// (https://www.alibabacloud.com/help/en/model-studio/model-pricing , 2026-11-04 核).
var summaryPrices = map[string][2]float64{
	"qwen-flash": {0.05, 0.4},
}

type summaryUsageLine struct {
	Time     string  `json:"time"`
	Model    string  `json:"model"`
	Input    int     `json:"input"`
	Output   int     `json:"output"`
	USD      float64 `json:"usd"`
	Unpriced bool    `json:"unpriced,omitempty"`
	Kind     string  `json:"kind"`
}

// NewSummarizer returns Options.Summarize: it asks a small text model to turn one Claude reply into two
// spoken sentences. Every call writes a usage line; a failure is logged ("summary fallback") and
// returned, and read_claude then falls back to the cut reply. key is looked up per call (a key stored after
// start works without a restart) and never logged; an empty key is an error.
func NewSummarizer(key func() string, model string, usage io.Writer, logger *slog.Logger) func(ctx context.Context, text string) (string, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if usage == nil {
		usage = io.Discard
	}
	return func(ctx context.Context, text string) (string, error) {
		var out string
		k := key()
		err := errors.New("no Qwen key on this Mac")
		if k != "" {
			out, err = summarizeOnce(ctx, k, model, truncRunes(cleanText(text), summaryInput), usage, logger)
		}
		if err != nil {
			logger.Warn("summary fallback", "model", model, "err", err)
		}
		return out, err
	}
}

func summarizeOnce(ctx context.Context, key, model, text string, usage io.Writer, logger *slog.Logger) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{
		"model":           model,
		"messages":        []map[string]string{{"role": "system", "content": summaryPrompt}, {"role": "user", "content": text}},
		"max_tokens":      120,
		"temperature":     0.2,
		"enable_thinking": false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, summaryURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncRunes(strings.TrimSpace(string(raw)), 200))
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", err
	}
	p, priced := summaryPrices[model]
	l := summaryUsageLine{Time: time.Now().Format(time.RFC3339), Model: model, Input: r.Usage.Prompt, Output: r.Usage.Completion,
		USD: (float64(r.Usage.Prompt)*p[0] + float64(r.Usage.Completion)*p[1]) / 1e6, Unpriced: !priced, Kind: "summary"}
	b, _ := json.Marshal(l)
	if _, err := usage.Write(append(b, '\n')); err != nil {
		logger.Warn("summary usage log write failed", "err", err)
	}
	if len(r.Choices) == 0 || strings.TrimSpace(r.Choices[0].Message.Content) == "" {
		return "", errors.New("empty answer")
	}
	return strings.TrimSpace(r.Choices[0].Message.Content), nil
}
