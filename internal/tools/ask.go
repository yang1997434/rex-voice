package tools

import (
	"regexp"
	"strconv"
	"strings"
)

const maxAskOptions = 6

// optionLine matches a numbered choice row of a Claude Code box: "❯ 1. Yes…" (the cursor row) or "  2. No…".
var optionLine = regexp.MustCompile(`^\s*(?:❯\s*)?([1-9])\.\s+(\S.*)$`)

// askMeta are the rows Claude Code appends to every AskUserQuestion; they are UI, not answers.
var askMeta = map[string]bool{"Type something.": true, "Chat about this": true}

// ParseOptions reads the numbered choices of the box at the bottom of a captured Claude screen
// (AskUserQuestion, plan approval, permission prompt; formats in testdata/capture_{ask,plan}.txt).
// options[i] is picked by key i+1. The last run starting at "1." wins, so numbered lists in Claude's
// earlier output are skipped. No box: nil.
func ParseOptions(screen string) []string {
	var opts []string
	for _, l := range strings.Split(screen, "\n") {
		m := optionLine.FindStringSubmatch(strings.ReplaceAll(l, " ", " "))
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		text := strings.TrimSpace(m[2])
		switch {
		case n == 1:
			opts = []string{text}
		case n == len(opts)+1:
			opts = append(opts, text)
		}
	}
	for len(opts) > 0 && askMeta[opts[len(opts)-1]] {
		opts = opts[:len(opts)-1]
	}
	if len(opts) > maxAskOptions {
		opts = opts[:maxAskOptions]
	}
	return opts
}
