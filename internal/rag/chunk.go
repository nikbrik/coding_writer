package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

func FixedChunks(docs []Document, target, overlap int, now time.Time) []Chunk {
	if target <= 0 {
		target = DefaultFixedTokens
	}
	if overlap < 0 {
		overlap = DefaultOverlapTokens
	}
	if overlap >= target {
		overlap = target / 5
	}
	var out []Chunk
	seqByPath := map[string]int{}
	for _, doc := range docs {
		lineTokens := tokenCountsByLine(doc.Lines)
		startLine := 1
		for startLine <= len(doc.Lines) {
			endLine, tokens := takeLines(lineTokens, startLine, target)
			if endLine < startLine {
				endLine = startLine
			}
			text := strings.Join(doc.Lines[startLine-1:endLine], "\n")
			if strings.TrimSpace(text) != "" {
				seqByPath[doc.Path]++
				out = append(out, makeChunk(doc, StrategyFixed, seqByPath[doc.Path], sectionForFixed(doc, startLine, endLine), startLine, endLine, text, tokens, now))
			}
			if endLine >= len(doc.Lines) {
				break
			}
			nextStart := overlapStart(lineTokens, startLine, endLine, overlap)
			if nextStart <= startLine {
				nextStart = endLine + 1
			}
			startLine = nextStart
		}
	}
	return out
}

func StructuralChunks(docs []Document, target, overlap int, now time.Time) []Chunk {
	var out []Chunk
	seqByPath := map[string]int{}
	for _, doc := range docs {
		var ranges []sectionRange
		switch strings.ToLower(filepath.Ext(doc.Path)) {
		case ".md":
			ranges = markdownSections(doc)
		case ".go":
			ranges = goSections(doc)
		default:
			ranges = paragraphSections(doc)
		}
		for _, r := range ranges {
			if r.end < r.start {
				continue
			}
			text := strings.Join(doc.Lines[r.start-1:r.end], "\n")
			if strings.TrimSpace(text) == "" {
				continue
			}
			if EstimateTokens(text) <= target || target <= 0 {
				seqByPath[doc.Path]++
				out = append(out, makeChunk(doc, StrategyStructural, seqByPath[doc.Path], r.section, r.start, r.end, text, EstimateTokens(text), now))
				continue
			}
			subDoc := doc
			subDoc.Lines = doc.Lines[r.start-1 : r.end]
			subDoc.Text = text
			subChunks := FixedChunks([]Document{subDoc}, target, overlap, now)
			for _, ch := range subChunks {
				seqByPath[doc.Path]++
				ch.ChunkID = chunkID(StrategyStructural, doc.Path, seqByPath[doc.Path])
				ch.Strategy = StrategyStructural
				ch.Section = r.section
				ch.StartLine += r.start - 1
				ch.EndLine += r.start - 1
				out = append(out, ch)
			}
		}
	}
	return out
}

type sectionRange struct {
	section string
	start   int
	end     int
}

func markdownSections(doc Document) []sectionRange {
	var starts []sectionRange
	for i, line := range doc.Lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if title == "" {
				title = doc.Title
			}
			starts = append(starts, sectionRange{section: title, start: i + 1})
		}
	}
	if len(starts) == 0 {
		return []sectionRange{{section: doc.Title, start: 1, end: len(doc.Lines)}}
	}
	if starts[0].start > 1 {
		starts = append([]sectionRange{{section: doc.Title, start: 1}}, starts...)
	}
	for i := range starts {
		if i+1 < len(starts) {
			starts[i].end = starts[i+1].start - 1
		} else {
			starts[i].end = len(doc.Lines)
		}
	}
	return starts
}

func goSections(doc Document) []sectionRange {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, doc.Path, doc.Text, parser.ParseComments)
	if err != nil || file == nil {
		return []sectionRange{{section: doc.Title, start: 1, end: len(doc.Lines)}}
	}
	var ranges []sectionRange
	for _, decl := range file.Decls {
		start := fset.Position(decl.Pos()).Line
		end := fset.Position(decl.End()).Line
		name := doc.Title
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name = d.Name.Name
		case *ast.GenDecl:
			if len(d.Specs) > 0 {
				switch spec := d.Specs[0].(type) {
				case *ast.TypeSpec:
					name = spec.Name.Name
				case *ast.ValueSpec:
					if len(spec.Names) > 0 {
						name = spec.Names[0].Name
					} else {
						name = d.Tok.String()
					}
				default:
					name = d.Tok.String()
				}
			} else {
				name = d.Tok.String()
			}
		}
		ranges = append(ranges, sectionRange{section: name, start: start, end: end})
	}
	if len(ranges) == 0 {
		return []sectionRange{{section: doc.Title, start: 1, end: len(doc.Lines)}}
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	return ranges
}

func paragraphSections(doc Document) []sectionRange {
	var out []sectionRange
	start := 1
	for i, line := range doc.Lines {
		if strings.TrimSpace(line) == "" {
			if i+1 > start {
				out = append(out, sectionRange{section: doc.Title, start: start, end: i})
			}
			start = i + 2
		}
	}
	if start <= len(doc.Lines) {
		out = append(out, sectionRange{section: doc.Title, start: start, end: len(doc.Lines)})
	}
	if len(out) == 0 {
		return []sectionRange{{section: doc.Title, start: 1, end: len(doc.Lines)}}
	}
	return out
}

func makeChunk(doc Document, strategy string, seq int, section string, start, end int, text string, tokens int, now time.Time) Chunk {
	sum := sha256.Sum256([]byte(text))
	if tokens <= 0 {
		tokens = EstimateTokens(text)
	}
	return Chunk{
		ChunkID:            chunkID(strategy, doc.Path, seq),
		Strategy:           strategy,
		Source:             doc.Source,
		Path:               doc.Path,
		Title:              doc.Title,
		Section:            section,
		StartLine:          start,
		EndLine:            end,
		Text:               text,
		TokenCountEstimate: tokens,
		ContentSHA256:      hex.EncodeToString(sum[:]),
		CreatedAt:          now,
	}
}

func chunkID(strategy, path string, seq int) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_").Replace(path)
	return fmt.Sprintf("%s:%s:%06d", strategy, safe, seq)
}

func EstimateTokens(text string) int {
	count := 0
	inToken := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if inToken {
				inToken = false
			}
			continue
		}
		if !inToken {
			count++
			inToken = true
		}
	}
	return count
}

func tokenCountsByLine(lines []string) []int {
	out := make([]int, len(lines)+1)
	for i, line := range lines {
		out[i+1] = EstimateTokens(line)
	}
	return out
}

func takeLines(lineTokens []int, start, target int) (int, int) {
	total := 0
	end := start
	for end < len(lineTokens) {
		total += lineTokens[end]
		if total >= target && end > start {
			return end, total
		}
		end++
	}
	return len(lineTokens) - 1, total
}

func overlapStart(lineTokens []int, start, end, overlap int) int {
	if overlap <= 0 {
		return end + 1
	}
	total := 0
	for line := end; line >= start; line-- {
		total += lineTokens[line]
		if total >= overlap {
			return line
		}
	}
	return end + 1
}

func sectionForFixed(doc Document, start, end int) string {
	if strings.ToLower(filepath.Ext(doc.Path)) != ".md" {
		return fmt.Sprintf("lines %d-%d", start, end)
	}
	section := doc.Title
	for i := 0; i < start && i < len(doc.Lines); i++ {
		trimmed := strings.TrimSpace(doc.Lines[i])
		if strings.HasPrefix(trimmed, "#") {
			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if title != "" {
				section = title
			}
		}
	}
	return section
}
