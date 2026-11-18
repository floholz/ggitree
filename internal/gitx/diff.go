package gitx

import (
	"context"
	"fmt"
	"html"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// maxDiffLines caps how much of one file's diff is sent to the page.
const maxDiffLines = 8000

// DiffLine is one line of a hunk. HTML is the syntax-highlighted text.
type DiffLine struct {
	Kind string `json:"k"` // " ", "+", "-" or "\\" (no newline marker)
	Old  int    `json:"o,omitempty"`
	New  int    `json:"n,omitempty"`
	HTML string `json:"h"`
}

// Hunk is one "@@" section of a diff.
type Hunk struct {
	Header string     `json:"header"`
	Lines  []DiffLine `json:"lines"`
}

// FileDiff is the diff of a single file.
type FileDiff struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
	Hunks     []Hunk `json:"hunks"`
}

// Diff returns the diff of one file in a commit (against its first parent),
// or of the working tree when sha is WorktreeSHA.
func (r *Repo) Diff(ctx context.Context, sha, path, oldPath string, untracked bool) (*FileDiff, error) {
	if !validRev(sha) {
		return nil, fmt.Errorf("invalid commit id %q", sha)
	}
	if path == "" || strings.Contains(path, "\x00") {
		return nil, fmt.Errorf("invalid path")
	}
	paths := []string{path}
	if oldPath != "" && oldPath != path {
		paths = append(paths, oldPath)
	}
	opts := []string{"diff", "-M", "-U3", "--no-color", "--no-ext-diff"}
	var out string
	var err error
	switch {
	case sha == WorktreeSHA && untracked:
		abs := filepath.Join(r.Root, path)
		if rel, e := filepath.Rel(r.Root, abs); e != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("invalid path")
		}
		// --no-index exits 1 when the files differ, which they always do here.
		out, err = r.git(ctx, append(opts, "--no-index", "--", "/dev/null", path)...)
		if out != "" {
			err = nil
		}
	case sha == WorktreeSHA:
		head, _ := r.Head(ctx)
		base := head.SHA
		if base == "" {
			base = EmptyTree
		}
		out, err = r.git(ctx, append(append(opts, base, "--"), paths...)...)
	default:
		base := EmptyTree
		if p, e := r.git(ctx, "rev-parse", "-q", "--verify", sha+"^1"); e == nil {
			base = strings.TrimSpace(p)
		}
		out, err = r.git(ctx, append(append(opts, base, sha, "--"), paths...)...)
	}
	if err != nil {
		return nil, err
	}
	d := parseDiff(out)
	d.Path, d.OldPath = path, oldPath
	highlight(d)
	return d, nil
}

func parseDiff(out string) *FileDiff {
	d := &FileDiff{}
	var cur *Hunk
	var oldN, newN, total int
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			d.Hunks = append(d.Hunks, Hunk{Header: line})
			cur = &d.Hunks[len(d.Hunks)-1]
			oldN, newN = parseHunkHeader(line)
		case cur == nil:
			if strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch") {
				d.Binary = true
			}
		case line == "":
			// Trailing newline at the very end of the output.
		default:
			if total >= maxDiffLines {
				d.Truncated = true
				continue
			}
			total++
			kind, text := line[:1], line[1:]
			dl := DiffLine{Kind: kind, HTML: text}
			switch kind {
			case " ":
				dl.Old, dl.New = oldN, newN
				oldN++
				newN++
			case "+":
				dl.New = newN
				newN++
			case "-":
				dl.Old = oldN
				oldN++
			case "\\":
			default:
				continue
			}
			cur.Lines = append(cur.Lines, dl)
		}
	}
	return d
}

// parseHunkHeader reads the start lines from "@@ -a,b +c,d @@".
func parseHunkHeader(h string) (int, int) {
	f := strings.Fields(h)
	if len(f) < 3 {
		return 0, 0
	}
	num := func(s string) int {
		s = strings.TrimLeft(s, "-+")
		s, _, _ = strings.Cut(s, ",")
		n, _ := strconv.Atoi(s)
		return n
	}
	return num(f[1]), num(f[2])
}

// highlight replaces each line's raw text with escaped, syntax-highlighted HTML.
// Each side of a hunk is tokenised as one piece of text, so constructs spanning
// several lines (comments, strings) keep their colors.
func highlight(d *FileDiff) {
	lexer := lexers.Match(filepath.Base(d.Path))
	for hi := range d.Hunks {
		h := &d.Hunks[hi]
		if lexer == nil {
			for i := range h.Lines {
				h.Lines[i].HTML = html.EscapeString(h.Lines[i].HTML)
			}
			continue
		}
		var oldIdx, newIdx []int
		var oldSrc, newSrc strings.Builder
		for i, l := range h.Lines {
			if l.Kind == " " || l.Kind == "-" {
				oldIdx = append(oldIdx, i)
				oldSrc.WriteString(l.HTML + "\n")
			}
			if l.Kind == " " || l.Kind == "+" {
				newIdx = append(newIdx, i)
				newSrc.WriteString(l.HTML + "\n")
			}
		}
		oldHTML := tokenLines(lexer, oldSrc.String(), len(oldIdx))
		newHTML := tokenLines(lexer, newSrc.String(), len(newIdx))
		for j, i := range oldIdx {
			h.Lines[i].HTML = oldHTML[j]
		}
		for j, i := range newIdx { // context lines take the new side's colors
			h.Lines[i].HTML = newHTML[j]
		}
		for i := range h.Lines {
			if h.Lines[i].Kind == "\\" {
				h.Lines[i].HTML = html.EscapeString(h.Lines[i].HTML)
			}
		}
	}
}

func tokenLines(lexer chroma.Lexer, src string, n int) []string {
	lines := make([]string, n)
	it, err := chroma.Coalesce(lexer).Tokenise(nil, src)
	if err != nil {
		for i, l := range strings.SplitN(src, "\n", n) {
			lines[i] = html.EscapeString(strings.TrimSuffix(l, "\n"))
		}
		return lines
	}
	var b strings.Builder
	li := 0
	for _, tok := range it.Tokens() {
		cls := tokenClass(tok.Type)
		parts := strings.Split(tok.Value, "\n")
		for pi, part := range parts {
			if pi > 0 {
				if li < n {
					lines[li] = b.String()
				}
				li++
				b.Reset()
			}
			if part == "" {
				continue
			}
			if cls == "" {
				b.WriteString(html.EscapeString(part))
			} else {
				b.WriteString(`<span class="` + cls + `">` + html.EscapeString(part) + `</span>`)
			}
		}
	}
	if li < n && b.Len() > 0 {
		lines[li] = b.String()
	}
	return lines
}

func tokenClass(t chroma.TokenType) string {
	for _, tt := range []chroma.TokenType{t, t.SubCategory(), t.Category()} {
		if c, ok := chroma.StandardTypes[tt]; ok && c != "" {
			return c
		}
	}
	return ""
}
