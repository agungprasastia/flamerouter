package rtk

import (
	"fmt"
	"strings"
)

// FilterSmartTruncate keeps head+tail of large blobs.
func FilterSmartTruncate(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) < SmartTruncateMin {
		return text
	}
	head := lines[:SmartTruncateHead]
	tail := lines[len(lines)-SmartTruncateTail:]
	omitted := len(lines) - SmartTruncateHead - SmartTruncateTail
	var b strings.Builder
	for _, l := range head {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf("... [%d lines omitted] ...\n", omitted))
	for i, l := range tail {
		b.WriteString(l)
		if i < len(tail)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// FilterDedupLog collapses consecutive duplicate lines.
func FilterDedupLog(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	var prev string
	count := 0
	flush := func() {
		if count == 0 {
			return
		}
		if count == 1 {
			out = append(out, prev)
		} else {
			out = append(out, fmt.Sprintf("%s  [×%d]", prev, count))
		}
	}
	for _, l := range lines {
		if len(l) > DedupLineMax {
			l = l[:DedupLineMax] + "…"
		}
		if l == prev {
			count++
			continue
		}
		flush()
		prev = l
		count = 1
	}
	flush()
	return strings.Join(out, "\n")
}

// FilterGitDiff keeps file headers + hunks capped.
func FilterGitDiff(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	hunkLines := 0
	inHunk := false
	for _, l := range lines {
		if strings.HasPrefix(l, "diff --git ") || strings.HasPrefix(l, "index ") ||
			strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ") {
			inHunk = false
			hunkLines = 0
			out = append(out, l)
			continue
		}
		if strings.HasPrefix(l, "@@ ") {
			inHunk = true
			hunkLines = 0
			out = append(out, l)
			continue
		}
		if inHunk {
			hunkLines++
			if hunkLines > GitDiffHunkMaxLines {
				if hunkLines == GitDiffHunkMaxLines+1 {
					out = append(out, "… [hunk truncated]")
				}
				continue
			}
			out = append(out, l)
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// FilterGitStatus caps file lists.
func FilterGitStatus(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	section := ""
	count := 0
	max := StatusMaxFiles
	for _, l := range lines {
		if strings.HasPrefix(l, "Untracked files:") {
			section = "untracked"
			count = 0
			max = StatusMaxUntracked
			out = append(out, l)
			continue
		}
		if strings.HasPrefix(l, "Changes ") || strings.HasPrefix(l, "On branch") || strings.HasPrefix(l, "nothing to commit") {
			section = "other"
			count = 0
			max = StatusMaxFiles
			out = append(out, l)
			continue
		}
		if section != "" && strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "(") {
			count++
			if count > max {
				if count == max+1 {
					out = append(out, fmt.Sprintf("  … and more"))
				}
				continue
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// FilterGitLog caps commit list.
func FilterGitLog(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= GitLogMaxLines {
		return text
	}
	return strings.Join(lines[:GitLogMaxLines], "\n") + fmt.Sprintf("\n… [%d lines truncated]", len(lines)-GitLogMaxLines)
}

// FilterGrep groups matches per file, caps per file.
func FilterGrep(text string) string {
	lines := strings.Split(text, "\n")
	perFile := map[string]int{}
	var out []string
	for _, l := range lines {
		if !isGrepLine(l) {
			out = append(out, l)
			continue
		}
		file := l[:strings.Index(l, ":")]
		perFile[file]++
		if perFile[file] > GrepPerFileMax {
			if perFile[file] == GrepPerFileMax+1 {
				out = append(out, fmt.Sprintf("%s:… [more matches truncated]", file))
			}
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// FilterFind caps files per directory.
func FilterFind(text string) string {
	lines := strings.Split(text, "\n")
	perDir := map[string]int{}
	dirs := 0
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		dir := l
		if i := strings.LastIndexAny(l, "/\\"); i >= 0 {
			dir = l[:i]
		}
		if perDir[dir] == 0 {
			dirs++
			if dirs > FindTotalDirMax {
				continue
			}
		}
		perDir[dir]++
		if perDir[dir] > FindPerDirMax {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// FilterLs summarizes noise dirs + caps listing.
func FilterLs(text string) string {
	lines := strings.Split(text, "\n")
	noise := map[string]bool{}
	for _, d := range LSNoiseDirs {
		noise[d] = true
	}
	var out []string
	skipped := 0
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 9 {
			name := fields[len(fields)-1]
			if noise[name] {
				skipped++
				continue
			}
		}
		out = append(out, l)
	}
	if skipped > 0 {
		out = append(out, fmt.Sprintf("… [%d noise dirs omitted]", skipped))
	}
	return strings.Join(out, "\n")
}

// FilterTree caps lines.
func FilterTree(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= TreeMaxLines {
		return text
	}
	return strings.Join(lines[:TreeMaxLines], "\n") + fmt.Sprintf("\n… [%d lines truncated]", len(lines)-TreeMaxLines)
}

// FilterSearchList caps per-dir results.
func FilterSearchList(text string) string {
	return FilterFind(text)
}

// FilterReadNumbered smart-truncates line-numbered dumps.
func FilterReadNumbered(text string) string {
	return FilterSmartTruncate(text)
}

// FilterBuildOutput keeps errors/warnings, drops noise.
func FilterBuildOutput(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	for _, l := range lines {
		lower := strings.ToLower(l)
		if strings.Contains(lower, "error") || strings.Contains(lower, "warn") ||
			strings.Contains(lower, "fail") || strings.Contains(lower, "success") ||
			strings.HasPrefix(strings.TrimSpace(l), "Compiling") ||
			strings.HasPrefix(strings.TrimSpace(l), "Finished") {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return FilterSmartTruncate(text)
	}
	return strings.Join(out, "\n")
}
