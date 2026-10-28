package extractor

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	ignore "github.com/sabhiram/go-gitignore"
)

const (
	DefaultMaxDepth = 120
	defaultOutName  = "TODO"
)

// DefaultTokens are the built-in search tokens.
var DefaultTokens = []string{"TODO", "FIXME"}

// DefaultGlobs exclude hidden / dunder names unless -H.
var DefaultGlobs = []string{".*", "__*"}

// Options is the extract configuration (CLI + library).
type Options struct {
	Path          string
	Out           string // empty => defaultOutName when not stdout-only
	Tokens        []string
	Exclude       []string
	MaxDepth      int // 0 => DefaultMaxDepth
	Stdout        bool
	UseGitignore  bool
	Tee           bool
	IncludeHidden bool
	FullPath      bool
	Concise       bool
	Recursive     bool
	IgnoreDefault bool
	Debug         bool
}

// --- tokens ---

// prepareTokenPattern builds a case-insensitive token regexp.
// Go RE2 has no lookbehind. Word boundaries are checked in findToken.
func prepareTokenPattern(tokens []string, ignoreDefault bool) (*regexp.Regexp, error) {
	var toks []string
	if !ignoreDefault {
		toks = append(toks, DefaultTokens...)
	}
	toks = append(toks, tokens...)
	if len(toks) == 0 {
		return nil, fmt.Errorf("no tokens to search for")
	}

	uniq := dedupeFirst(toks)
	slices.SortStableFunc(uniq, func(a, b string) int {
		return cmp.Compare(len(b), len(a)) // longest first
	})

	alts := make([]string, len(uniq))
	for i, t := range uniq {
		alts[i] = regexp.QuoteMeta(t)
	}
	return regexp.Compile("(?i)(?:" + strings.Join(alts, "|") + ")")
}

// tokenHit is one hit: original-casing span from the line.
type tokenHit struct {
	Token string
	Start int
	End   int
}

// findToken returns the first token match on line, or nil.
// Prefer leftmost, then longest (re alts are longest-first).
func findToken(re *regexp.Regexp, line string) *tokenHit {
	for _, loc := range re.FindAllStringIndex(line, -1) {
		start, end := loc[0], loc[1]
		if !isWordBoundary(line, start, end) {
			continue
		}
		return &tokenHit{Token: line[start:end], Start: start, End: end}
	}
	return nil
}

func isWordBoundary(line string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(line[:start])
		if isWordRune(r) {
			return false
		}
	}
	if end < len(line) {
		r, _ := utf8.DecodeRuneInString(line[end:])
		if isWordRune(r) {
			return false
		}
	}
	return true
}

// isWordRune is a word char: Unicode letter, digit, or underscore.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// --- snippet ---

// errInvalidSnippetCount is the base for invalid `{N}` after a token.
var errInvalidSnippetCount = errors.New("invalid snippet count")

// snippetCount parses optional `{N}` immediately after the matched token.
//
// tokenEnd is the byte index right after the token. It must land on '{' to
// parse a count. If line[tokenEnd] is not '{', returns 1.
//
// {N} means N total lines in the snippet (token line + N-1 following reads).
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

// expand appends up to n-1 following lines without further token Find.
// bad is the invalid-UTF-8 line when a consumed line fails decode (not appended).
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

// indent prefixes non-blank lines only.
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

// dedent strips the common leading whitespace from non-blank lines.
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

// --- exclude ---

// excludeMatch reports whether rel (slash-separated, relative to scan root) is excluded.
// Dir exclude needs trailing slash: check both rel and rel+"/".
func excludeMatch(gi *ignore.GitIgnore, rel string, isDir bool) bool {
	if isDir {
		// Pattern "vendor/" matches "vendor/" but not bare "vendor".
		return gi.MatchesPath(rel) || gi.MatchesPath(rel+"/")
	}
	return gi.MatchesPath(rel)
}

// prepareExcludeSpec builds the exclude GitIgnore:
//
//	order: DefaultGlobs (unless includeHidden) -> .gitignore (if -g and root is dir) -> -x
//	dedupe preserving order (first wins). Blank/#/whitespace lines are no-ops.
func prepareExcludeSpec(root string, exclude []string, includeHidden, useGitignore bool) (*ignore.GitIgnore, error) {
	var lines []string
	if !includeHidden {
		lines = append(lines, DefaultGlobs...)
	}
	if useGitignore {
		fi, err := os.Stat(root)
		if err == nil && fi.IsDir() {
			gi, err := getGitignorePatterns(root)
			if err != nil {
				return nil, err
			}
			lines = append(lines, gi...)
		}
	}
	lines = append(lines, exclude...)
	return ignore.CompileIgnoreLines(dedupeFirst(lines)...), nil
}

// dedupeFirst keeps the first occurrence of each identical pattern string.
func dedupeFirst(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// getGitignorePatterns reads root/.gitignore (splitlines).
// Missing path or non-regular file -> empty slice (no error).
func getGitignorePatterns(root string) ([]string, error) {
	p := filepath.Join(root, ".gitignore")
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return splitlines(data), nil
}

// isExcluded reports whether path should be skipped during the walk.
func (e *Extractor) isExcluded(path string, isDir bool) bool {
	rel, err := filepath.Rel(e.root, path)
	if err != nil {
		// Relative path vs abs root (chdir): still honor out skip.
		return e.isOutPath(path)
	}
	rel = filepath.ToSlash(rel)
	// Never exclude scan root — DefaultGlobs ".*" matches ".".
	// Root wins over out-path skip (scan root that is also -o is still processed).
	if rel == "." || strings.HasPrefix(rel, "../") {
		return false
	}
	// Skip the configured out file (abs path). Stdout-only never skips.
	if e.isOutPath(path) {
		return true
	}
	return excludeMatch(e.gi, rel, isDir)
}

// isOutPath reports whether path is the configured out file (Abs equality).
// Stdout-only / empty outAbs => never skip. outAbs is cached once in setOut.
func (e *Extractor) isOutPath(path string) bool {
	if e.outAbs == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	return err == nil && absPath == e.outAbs
}

// splitlines splits on \n / \r\n / \r. No keepends. No trailing empty from a
// final line break.
func splitlines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := string(data)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}
