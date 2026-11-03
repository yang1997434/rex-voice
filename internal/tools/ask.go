package tools

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxAskOptions = 6

// optionLine matches a numbered choice row of a Claude Code box: "❯ 1. Yes…" (the cursor row) or "  2. No…".
var optionLine = regexp.MustCompile(`^\s*(?:❯\s*)?([1-9])\.\s+(\S.*)$`)

// ParseBox reads the box at the bottom of a captured Claude screen (AskUserQuestion, plan approval,
// permission prompt; formats in testdata/capture_{ask,plan}.txt and twoQuestions in watch_test.go).
// options[i] is picked by key i+1. The last run starting at "1." wins, so numbered lists in Claude's
// earlier output are skipped.
// question is the text above "1." (wrapped lines joined), only for an AskUserQuestion (recognised by
// its trailing "Type something." rows): the question on screen now, which with several questions is
// not the one program_status names. No box: "", nil.
func ParseBox(screen string) (question string, opts []string) {
	var above []string // the last paragraph above the current line, after the last rule / tab bar / option
	gap := false       // a blank line since the last text: the next text starts a new paragraph
	for _, l := range strings.Split(screen, "\n") {
		l = strings.ReplaceAll(l, "\u00a0", " ") // Claude Code pads some rows with NBSP
		m := optionLine.FindStringSubmatch(l)
		if m == nil {
			switch t := strings.TrimSpace(l); {
			case t == "":
				gap = true
			case strings.HasPrefix(t, "─") || strings.ContainsAny(t, "☐☒✔"):
				above, gap = above[:0], false
			default:
				if gap {
					above, gap = above[:0], false
				}
				above = append(above, t)
			}
			continue
		}
		n, _ := strconv.Atoi(m[1])
		text := strings.TrimSpace(m[2])
		switch {
		case n == 1:
			opts = []string{text}
			question = joinWrapped(above)
		case n == len(opts)+1:
			opts = append(opts, text)
		}
		above, gap = above[:0], false
	}
	ask := false
	if n := len(opts); n > 0 && opts[n-1] == "Chat about this" {
		opts, ask = opts[:n-1], true
	}
	// The row above "Chat about this" is the free-text field: "Type something." until the user types
	// into it, then whatever they typed so far (a changing "option" would re-notify every round).
	if n := len(opts); n > 0 && (ask || opts[n-1] == "Type something.") {
		opts, ask = opts[:n-1], true
	}
	if !ask {
		question = ""
	}
	// After the last of several questions Claude Code shows a "Submit answers / Cancel" page with no question
	// text; without a title the phone would keep the previous question's.
	if question == "" && len(opts) > 0 && strings.HasPrefix(opts[0], "Submit") {
		question = "提交答案？"
	}
	if len(opts) > maxAskOptions {
		opts = opts[:maxAskOptions]
	}
	return question, opts
}

// joinWrapped undoes the terminal's line wrap: a space only between two ASCII ends, so CJK stays unspaced.
func joinWrapped(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		if b.Len() > 0 {
			last, _ := utf8.DecodeLastRuneInString(b.String())
			first, _ := utf8.DecodeRuneInString(l)
			if last < utf8.RuneSelf && first < utf8.RuneSelf {
				b.WriteByte(' ')
			}
		}
		b.WriteString(l)
	}
	return b.String()
}
