package reader

import (
	"math"
	"sort"
	"strings"
)

// splice is a byte-range replacement of a page's flat text: the bytes
// [ByteStart, ByteEnd) are replaced by Replacement. Used by detectTables (T017)
// to swap a table region's flat text for a rendered Markdown table.
type splice struct {
	ByteStart, ByteEnd int
	Replacement        string
}

// ptRow is a Y-band of fragments sharing a baseline (reading order = high Y first).
type ptRow struct {
	anchorY float64
	frags   []positionedText
}

// detectTables runs page-local grid detection (spec 031 T017). Returns zero or
// more splices (empty -> caller emits flat text). CONSERVATIVE by design: a
// false-positive table garbles searchable text, which is worse than a missed
// table. Any signal that the region is not a clean ≥3-column grid -> nil.
func detectTables(frags []positionedText) []splice {
	if len(frags) < 4 {
		return nil
	}
	minFS, medFS, maxFS := fontStats(frags)
	if medFS <= 0 {
		minFS, medFS, maxFS = 12, 12, 12 // unknown sizes -> default pitch tolerance
	}
	if maxFS > 3*minFS {
		return nil // mixed sizes (caption + body, title + table) -> bail
	}
	rowTol := math.Max(2.0, 0.5*medFS)
	rows := bandRows(frags, rowTol)
	if len(rows) < 2 || len(rows) > 60 {
		return nil
	}
	lo, hi := longestUniformRun(rows, 0.9*minFS, 3*maxFS)
	if hi-lo < 3 {
		return nil // require >=3 rows (header + >=2 data rows); a 2-row region is too eager
	}
	return detectTablesInRun(rows[lo:hi], medFS)
}

// detectTablesInRun runs column detection + cell assembly on a uniform-pitch run
// of rows and returns a splice (or nil). Applies the full conservative gate.
func detectTablesInRun(rows []ptRow, medFS float64) []splice {
	colTol := math.Max(2.0, 0.25*medFS)
	var all []positionedText
	for _, r := range rows {
		all = append(all, r.frags...)
	}
	anchors := clusterX(all, colTol)
	nCols := len(anchors)
	if nCols < 3 || nCols > 12 {
		return nil // require >=3 columns (kills numbered lists / definition lists)
	}
	// column-span: real table columns are well separated (>=3*medFS apart). Tight
	// adjacent gaps mean code-block indentation stops, not columns (design-verdict
	// HIGH fix). clusterX returns anchors ascending.
	for i := 0; i+1 < len(anchors); i++ {
		if anchors[i+1]-anchors[i] < 3*medFS {
			return nil
		}
	}
	nRows := len(rows)
	grid := make([][]string, nRows)
	for i := range grid {
		grid[i] = make([]string, nCols)
	}
	colRows := make([]int, nCols)
	colCellLen := make([][]int, nCols)
	assigned := 0
	totalCellLen, totalCells := 0, 0
	for ri, r := range rows {
		// a row cell may span multiple frags; assemble by nearest column anchor
		cellTexts := make(map[int][]string)
		for _, f := range r.frags {
			ci := nearestAnchor(anchors, f.X)
			if math.Abs(f.X-anchors[ci]) > colTol {
				return nil // stray fragment -> grid contaminated -> bail (merged cells included)
			}
			cellTexts[ci] = append(cellTexts[ci], f.Text)
		}
		for ci, texts := range cellTexts {
			cell := strings.Join(texts, " ")
			if len(cell) > 200 {
				return nil // a "cell" over 200 chars is prose masquerading
			}
			grid[ri][ci] = cell
			colRows[ci]++
			colCellLen[ci] = append(colCellLen[ci], len(cell))
			assigned++
			if cell != "" {
				totalCellLen += len(cell)
				totalCells++
			}
		}
	}
	// mean-cell-length: multi-column body text (newsletter) has long sentence "cells"
	// with uniform length across columns, so the asymmetry check (which compares
	// columns to each other) misses it. Real table cells average short (design-verdict
	// HIGH fix).
	if totalCells > 0 && float64(totalCellLen)/float64(totalCells) > 15+medFS {
		return nil
	}
	// column reuse: >=2 columns each populated by >=2 rows
	reuse := 0
	for _, c := range colRows {
		if c >= 2 {
			reuse++
		}
	}
	if reuse < 2 {
		return nil
	}
	// fill ratio
	if float64(assigned)/float64(nRows*nCols) < 0.40 {
		return nil
	}
	// column asymmetry: if one column's median cell length is >4x another's it is
	// prose-asymmetric (definition list, margin-number + body) -> bail.
	if medians := colMedians(colCellLen); len(medians) > 0 {
		minM, maxM := medians[0], medians[0]
		for _, m := range medians {
			if m < minM {
				minM = m
			}
			if m > maxM {
				maxM = m
			}
		}
		if minM > 0 && maxM > 4*minM {
			return nil
		}
	}
	md := renderMarkdownTable(grid)
	if md == "" {
		return nil
	}
	bs, be := all[0].ByteStart, all[0].ByteEnd
	for _, f := range all {
		if f.ByteStart < bs {
			bs = f.ByteStart
		}
		if f.ByteEnd > be {
			be = f.ByteEnd
		}
	}
	return []splice{{ByteStart: bs, ByteEnd: be, Replacement: "\n" + md + "\n"}}
}

// renderMarkdownTable renders the grid as a GitHub-Flavored Markdown table. The
// first row is the header; if it is sparse (<50% populated) the table is rejected
// (empty string) — a units/spanning row is not a header.
func renderMarkdownTable(grid [][]string) string {
	if len(grid) == 0 {
		return ""
	}
	nCols := len(grid[0])
	clean := func(s string) string {
		s = strings.TrimSpace(s)
		s = collapseSpaces(s)            // collapse 2+ whitespace -> 1 (keep the join space)
		s = strings.ReplaceAll(s, "|", "\\|")
		return s
	}
	populated := func(row []string) int {
		n := 0
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				n++
			}
		}
		return n
	}
	header := grid[0]
	if populated(header)*2 < nCols {
		return "" // sparse header -> bail
	}
	var b strings.Builder
	b.WriteString("| ")
	for i, c := range header {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(clean(c))
	}
	b.WriteString(" |\n|")
	for range header {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for _, row := range grid[1:] {
		b.WriteString("| ")
		for i, c := range row {
			if i > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(clean(c))
		}
		b.WriteString(" |\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func collapseSpaces(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

// bandRows groups fragments into Y-bands (rows), sorted Y DESCENDING (PDF Y-up ->
// reading order is high Y first). Row anchor Y is the first fragment's Y (no
// running-mean drift — design verdict fix).
func bandRows(frags []positionedText, tol float64) []ptRow {
	sorted := make([]positionedText, len(frags))
	copy(sorted, frags)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Y > sorted[j].Y })
	var rows []ptRow
	for _, f := range sorted {
		if len(rows) > 0 && math.Abs(rows[len(rows)-1].anchorY-f.Y) <= tol {
			rows[len(rows)-1].frags = append(rows[len(rows)-1].frags, f)
		} else {
			rows = append(rows, ptRow{anchorY: f.Y, frags: []positionedText{f}})
		}
	}
	return rows
}

// longestUniformRun returns the longest half-open range [lo,hi) of consecutive
// rows whose pairwise gaps are all in [loGap,hiGap] and whose max/min gap <= 1.5
// (pitch uniformity). Single pass; ties go to the earlier run.
func longestUniformRun(rows []ptRow, loGap, hiGap float64) (int, int) {
	if len(rows) < 2 {
		return 0, 0
	}
	bestLo, bestHi := 0, 0
	curLo := 0
	for i := 1; i < len(rows); i++ {
		gap := rows[i-1].anchorY - rows[i].anchorY
		ok := gap >= loGap && gap <= hiGap
		if ok {
			// uniformity: max/min gap over the current run [curLo,i] <= 1.5
			mn, mx := gap, gap
			for k := curLo; k < i; k++ {
				g := rows[k].anchorY - rows[k+1].anchorY
				if g < mn {
					mn = g
				}
				if g > mx {
					mx = g
				}
			}
			if mn > 0 && mx/mn > 1.5 {
				ok = false
			}
		}
		if !ok {
			if i-curLo > bestHi-bestLo {
				bestLo, bestHi = curLo, i
			}
			curLo = i
		}
	}
	if len(rows)-curLo > bestHi-bestLo {
		bestLo, bestHi = curLo, len(rows)
	}
	return bestLo, bestHi
}

// clusterX 1D-clusters fragment start-X values greedily (sorted); a new cluster
// opens when an X exceeds the running cluster max by more than tol. Returns the
// cluster anchor Xs (medians), sorted ascending.
func clusterX(frags []positionedText, tol float64) []float64 {
	xs := make([]float64, 0, len(frags))
	for _, f := range frags {
		xs = append(xs, f.X)
	}
	sort.Float64s(xs)
	var clusters [][]float64
	for _, x := range xs {
		if len(clusters) == 0 || x-clusterMax(clusters[len(clusters)-1]) > tol {
			clusters = append(clusters, []float64{x})
		} else {
			clusters[len(clusters)-1] = append(clusters[len(clusters)-1], x)
		}
	}
	anchors := make([]float64, len(clusters))
	for i, c := range clusters {
		anchors[i] = median(c)
	}
	return anchors
}

func clusterMax(c []float64) float64 {
	m := c[0]
	for _, v := range c[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func nearestAnchor(anchors []float64, x float64) int {
	if len(anchors) == 0 {
		return 0 // defensive: never index an empty anchor slice (Constitution: never panic)
	}
	best := 0
	bestD := math.Abs(x - anchors[0])
	for i := 1; i < len(anchors); i++ {
		d := math.Abs(x - anchors[i])
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// colMedians returns the median non-empty cell length per column (columns with no
// cells contribute nothing).
func colMedians(colCellLen [][]int) []float64 {
	var out []float64
	for _, lens := range colCellLen {
		if len(lens) == 0 {
			continue
		}
		s := append([]int(nil), lens...)
		sort.Ints(s)
		m := float64(s[len(s)/2])
		out = append(out, m)
	}
	return out
}

// fontStats returns min, median, max of non-zero fragment font sizes. All zero
// if no fragment has a known size.
func fontStats(frags []positionedText) (min, med, max float64) {
	var sizes []float64
	for _, f := range frags {
		if f.FontSize > 0 {
			sizes = append(sizes, f.FontSize)
		}
	}
	if len(sizes) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(sizes)
	min, max = sizes[0], sizes[len(sizes)-1]
	med = sizes[len(sizes)/2]
	return
}

// renderPageWithTables applies detectTables splices to the page's flat text,
// right-to-left by ByteStart so earlier offsets stay valid.
func renderPageWithTables(flat string, frags []positionedText) string {
	splices := detectTables(frags)
	if len(splices) == 0 {
		return flat
	}
	sort.Slice(splices, func(i, j int) bool { return splices[i].ByteStart > splices[j].ByteStart })
	out := flat
	for _, sp := range splices {
		if sp.ByteStart < 0 || sp.ByteEnd > len(out) || sp.ByteStart > sp.ByteEnd {
			continue // defensive: never splice out of range
		}
		out = out[:sp.ByteStart] + sp.Replacement + out[sp.ByteEnd:]
	}
	return out
}
