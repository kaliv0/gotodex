package extractor

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

var errInvalidSnippetCount = errors.New("invalid snippet count")

// snippetCount parses optional {N} after the token. No brace => 1.
// {N} is total lines in the snippet (token line + N-1 following).
func snippetCount(line string, tokenEnd int) (int, error) {
	if tokenEnd < 0 || tokenEnd >= len(line) || line[tokenEnd] != '{' {
		return 1, nil
	}
	body, _, ok := strings.Cut(line[tokenEnd+1:], "}")
	if !ok {
		return 0, fmt.Errorf("%w: unclosed '{'", errInvalidSnippetCount)
	}
	n, err := strconv.Atoi(body)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", errInvalidSnippetCount, body)
	}
	return n, nil
}

// expand appends up to n-1 following lines. bad is set on invalid UTF-8.
func expand(br *bufio.Reader, line string, lineNo, n int) (joined string, newLineNo int, bad string) {
	for i := 1; i < n; i++ {
		next, err := br.ReadString('\n')
		if next == "" {
			break
		}
		if !utf8.ValidString(next) {
			return line, lineNo, next
		}
		line += next
		lineNo++
		if err != nil {
			break
		}
	}
	return line, lineNo, ""
}

func indent(text, prefix string) string {
	if text == "" {
		return text
	}
	var b strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		if strings.TrimSpace(line) != "" {
			b.WriteString(prefix)
		}
		b.WriteString(line)
	}
	return b.String()
}

func dedent(text string) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	common := ""
	found := false
	for i, line := range lines {
		if trailingSplitEmpty(lines, i) || strings.TrimSpace(line) == "" {
			continue
		}
		ws := leadingWS(line)
		if !found {
			common, found = ws, true
			continue
		}
		common = commonPrefix(common, ws)
		if common == "" {
			return text
		}
	}
	if !found || common == "" {
		return text
	}
	for i, line := range lines {
		if trailingSplitEmpty(lines, i) {
			continue
		}
		lines[i] = strings.TrimPrefix(line, common)
	}
	return strings.Join(lines, "\n")
}

func trailingSplitEmpty(lines []string, i int) bool {
	return i == len(lines)-1 && lines[i] == ""
}

func leadingWS(s string) string {
	i := 0
	for i < len(s) {
		c := s[i]
		if c != ' ' && c != '\t' {
			break
		}
		i++
	}
	return s[:i]
}

func commonPrefix(a, b string) string {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}
