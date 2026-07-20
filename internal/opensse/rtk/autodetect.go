package rtk

import (
	"regexp"
	"strings"
)

var (
	reGitDiff     = regexp.MustCompile(`(?m)^diff --git `)
	reGitDiffHunk = regexp.MustCompile(`(?m)^@@ `)
	reGitStatus   = regexp.MustCompile(`(?m)^On branch |^nothing to commit|^Changes (not |to be )|^Untracked files:`)
	reGitLog      = regexp.MustCompile(`(?m)^[*|/\\ ]*commit [0-9a-f]{7,40}$`)
	rePorcelain   = regexp.MustCompile(`(?m)^[ MADRCU?!][ MADRCU?!] \S`)
	reBuildOutput = regexp.MustCompile(`(?im)^(npm (warn|error|ERR!)|yarn (warn|error)|\s*Compiling\s+\S+|\s*Downloading\s+\S+|added \d+ package|\[ERROR\]|BUILD (SUCCESS|FAILED)|\s*Finished\s+|Successfully (installed|built)|ERROR:)`)
	reTreeGlyph   = regexp.MustCompile(`[├└]──|│  `)
	reLsRow       = regexp.MustCompile(`(?m)^[-dlbcps][rwx-]{9}`)
	reLsTotal     = regexp.MustCompile(`(?m)^total \d+$`)
	reSearchList  = regexp.MustCompile(`Result of search in .+ \(total \d+ files?\):`)
	reReadNumbered = regexp.MustCompile(`^\s*\d+\|`)
	reGrepLine    = regexp.MustCompile(`^[^:]+:\d+:`)
)

// AutoDetectFilter returns a filter function + name, or nil.
func AutoDetectFilter(text string) (func(string) string, string) {
	head := text
	if len(head) > DetectWindow {
		head = head[:DetectWindow]
	}
	if reGitLog.MatchString(head) {
		return FilterGitLog, "git-log"
	}
	if reGitDiff.MatchString(head) || reGitDiffHunk.MatchString(head) {
		return FilterGitDiff, "git-diff"
	}
	if reGitStatus.MatchString(head) {
		return FilterGitStatus, "git-status"
	}
	if reBuildOutput.MatchString(head) {
		return FilterBuildOutput, "build-output"
	}
	if isMostlyPorcelain(head) {
		return FilterGitStatus, "git-status"
	}
	lines := strings.Split(head, "\n")
	var nonEmpty []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	first5 := nonEmpty
	if len(first5) > 5 {
		first5 = first5[:5]
	}
	for _, l := range first5 {
		if isGrepLine(l) {
			return FilterGrep, "grep"
		}
	}
	if len(nonEmpty) >= 3 && allPathLike(nonEmpty) {
		return FilterFind, "find"
	}
	if reTreeGlyph.MatchString(head) {
		return FilterTree, "tree"
	}
	if reLsTotal.MatchString(head) || countMatches(head, reLsRow) >= 3 {
		return FilterLs, "ls"
	}
	if reSearchList.MatchString(head) {
		return FilterSearchList, "search-list"
	}
	fullLines := strings.Split(text, "\n")
	if len(fullLines) >= SmartTruncateMin && isLineNumbered(fullLines) {
		return FilterReadNumbered, "read-numbered"
	}
	if len(nonEmpty) >= 5 {
		return FilterDedupLog, "dedup-log"
	}
	if len(fullLines) >= SmartTruncateMin {
		return FilterSmartTruncate, "smart-truncate"
	}
	return nil, ""
}

func isGrepLine(line string) bool {
	first := strings.Index(line, ":")
	if first < 0 {
		return false
	}
	second := strings.Index(line[first+1:], ":")
	if second < 0 {
		return false
	}
	lineno := line[first+1 : first+1+second]
	for _, c := range lineno {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(lineno) > 0
}

func isPathLike(line string) bool {
	if strings.Contains(line, ":") {
		return false
	}
	return strings.Contains(line, "/") || strings.Contains(line, "\\") || strings.HasPrefix(line, ".")
}

func allPathLike(lines []string) bool {
	for _, l := range lines {
		if !isPathLike(l) {
			return false
		}
	}
	return true
}

func isMostlyPorcelain(head string) bool {
	lines := strings.Split(head, "\n")
	n, match := 0, 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n++
		if rePorcelain.MatchString(l) {
			match++
		}
	}
	return n >= 3 && match*2 >= n
}

func countMatches(s string, re *regexp.Regexp) int {
	return len(re.FindAllString(s, -1))
}

func isLineNumbered(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	hits := 0
	for _, l := range lines {
		if reReadNumbered.MatchString(l) {
			hits++
		}
	}
	return float64(hits)/float64(len(lines)) >= ReadNumberedMinHit
}
