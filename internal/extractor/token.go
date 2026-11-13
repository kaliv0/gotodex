package extractor

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

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

type tokenHit struct {
	Token string
	Start int
	End   int
}

// findToken returns the first match (leftmost, then longest), or nil.
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

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

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
