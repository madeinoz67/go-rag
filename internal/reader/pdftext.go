package reader

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// positionedText is one decoded glyph run with its text-space start position,
// font, and the byte range it occupies in the page's flat extracted text.
// Produced by parsePositionedText (spec 031 T004); consumed by detectTables
// (T017) and the font-size heading fallback. ByteStart/ByteEnd index into the
// doc-ordered flat string parsePositionedText builds IN LOCKSTEP — assigned by
// construction, never by re-scanning (design-verdict critical fix #1: the legacy
// extractShowText emits Tj-then-TJ, not document order, so re-scanning would
// silently mis-assign ranges and splice table Markdown into the wrong bytes).
type positionedText struct {
	Text               string
	X, Y               float64 // text-space start = (Tm.e, Tm.f); PDF Y-up (consumer flips for reading order)
	FontSize           float64 // Tf size operand; 0 = unknown
	Font               string  // Tf /Name (slash stripped)
	ByteStart, ByteEnd int     // byte offsets into parsePositionedText's flat output
}

// mat is a 2x3 affine text matrix [a b c d e f]; origin = (e, f).
type mat struct{ a, b, c, d, e, f float64 }

func identityMat() mat { return mat{a: 1, d: 1} }

// translate multiplies this (line) matrix by a translation — Td/TD/T* semantics
// (§9.4.3): the line matrix is multiplied, then the caller copies it to the text
// matrix. Translation in text space: origin moves by (a*tx+c*ty, b*tx+d*ty); for
// the common non-rotated case that is (tx, ty).
func (m mat) translate(tx, ty float64) mat {
	return mat{a: m.a, b: m.b, c: m.c, d: m.d, e: m.e + m.a*tx + m.c*ty, f: m.f + m.b*tx + m.d*ty}
}

// rotated reports a non-trivial rotation/skew (b or c non-zero). Such fragments
// are skipped at emit time — rotated text breaks grid alignment (design verdict).
func (m mat) rotated() bool {
	const eps = 0.01
	return absVal(m.b) > eps || absVal(m.c) > eps
}

func absVal(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// --- tokenizer (postfix operands + operator) ---

type tokKind uint8

const (
	tNum  tokKind = iota
	tStr          // literal or hex string, decoded
	tName         // /Foo
	tArr          // TJ array
	tOp           // operator word (Tj, TJ, Tm, ...)
)

type arrEl struct {
	isStr bool
	str   string
	num   float64
}

type token struct {
	kind  tokKind
	num   float64
	str   string
	elems []arrEl
}

func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '\f', 0:
		return true
	}
	return false
}

func isDelim(c byte) bool {
	return isSpace(c) || c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' ||
		c == '{' || c == '}' || c == '/' || c == '%'
}

func isDigit(c byte) bool    { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool    { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isNumStart(c byte) bool { return c == '+' || c == '-' || c == '.' || isDigit(c) }

func readNumberToken(s string, i int) (float64, int) {
	n := len(s)
	j := i
	// optional leading sign
	if j < n && (s[j] == '+' || s[j] == '-') {
		j++
	}
	// integer part (digits)
	for j < n && isDigit(s[j]) {
		j++
	}
	// fractional part (. digits)
	if j < n && s[j] == '.' {
		j++
		for j < n && isDigit(s[j]) {
			j++
		}
	}
	// exponent (e/E optional-sign digits)
	if j < n && (s[j] == 'e' || s[j] == 'E') {
		j++
		if j < n && (s[j] == '+' || s[j] == '-') {
			j++
		}
		for j < n && isDigit(s[j]) {
			j++
		}
	}
	f, _ := strconv.ParseFloat(s[i:j], 64)
	return f, j
}

// tokenize splits a decompressed PDF content stream into postfix tokens. Handles
// literal strings (balanced parens + escapes incl. octal), hex strings, names,
// arrays, numbers, and operators; skips comments (% EOL), dict marks (<<...>>),
// and inline images (BI...EI, whose raw bytes would otherwise parse as fake
// operators and poison the page — design verdict critical fix #3). Best-effort
// on malformed input.
func tokenize(s string) []token {
	var toks []token
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		if isSpace(c) {
			i++
			continue
		}
		if c == '%' { // comment to EOL
			for i < n && s[i] != '\n' && s[i] != '\r' {
				i++
			}
			continue
		}
		switch c {
		case '(':
			str, next := readLiteral(s, i+1)
			toks = append(toks, token{kind: tStr, str: str})
			i = next
			continue
		case '<':
			if i+1 < n && s[i+1] == '<' { // dict mark — skip to matching >>
				i += 2
				depth := 1
				for i < n && depth > 0 {
					if i+1 < n && s[i] == '<' && s[i+1] == '<' {
						depth++
						i += 2
					} else if i+1 < n && s[i] == '>' && s[i+1] == '>' {
						depth--
						i += 2
					} else {
						i++
					}
				}
				continue
			}
			str, next := readHex(s, i+1)
			toks = append(toks, token{kind: tStr, str: str})
			i = next
			continue
		case '[':
			elems, next := readArray(s, i+1)
			toks = append(toks, token{kind: tArr, elems: elems})
			i = next
			continue
		case '/':
			j := i + 1
			for j < n && !isDelim(s[j]) {
				j++
			}
			toks = append(toks, token{kind: tName, str: s[i+1 : j]})
			i = j
			continue
		}
		if isNumStart(c) {
			num, next := readNumberToken(s, i)
			toks = append(toks, token{kind: tNum, num: num})
			i = next
			continue
		}
		if isAlpha(c) || c == '\'' || c == '"' {
			j := i + 1
			for j < n && !isDelim(s[j]) {
				j++
			}
			op := s[i:j]
			if op == "BI" { // inline image: skip raw bytes to the EI keyword
				i = skipInlineImage(s, j)
				continue
			}
			toks = append(toks, token{kind: tOp, str: op})
			i = j
			continue
		}
		i++ // unknown single char
	}
	return toks
}

// skipInlineImage advances past an inline image (BI keyvals ID <bytes> EI) to the
// index after a whitespace-preceded "EI". Image bytes may contain anything; this
// is best-effort (design verdict: rare in table PDFs, but must not poison parsing).
func skipInlineImage(s string, i int) int {
	n := len(s)
	for i < n {
		idx := strings.Index(s[i:], "EI")
		if idx < 0 {
			return n
		}
		pre := i + idx
		end := pre + 2
		if pre == 0 || isSpace(s[pre-1]) {
			return end
		}
		i = end
	}
	return n
}

// readLiteral reads a PDF literal string after the opening '('. Handles nested
// balanced parens and backslash escapes incl. octal \ddd (§7.9.2). Returns the
// decoded string and the index after the closing ')'.
func readLiteral(s string, i int) (string, int) {
	var b strings.Builder
	depth, n := 1, len(s)
	for i < n {
		c := s[i]
		if c == '\\' {
			i++
			if i >= n {
				break
			}
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
				i++
			case 'r':
				b.WriteByte('\r')
				i++
			case 't':
				b.WriteByte('\t')
				i++
			case 'b':
				b.WriteByte('\b')
				i++
			case 'f':
				b.WriteByte('\f')
				i++
			case '(':
				b.WriteByte('(')
				i++
			case ')':
				b.WriteByte(')')
				i++
			case 0x5C: // backslash
				b.WriteByte(0x5C)
				i++
			case '\n':
				i++ // line continuation
			default:
				if isDigit(s[i]) { // octal \ddd (1-3 digits)
					val := s[i] - '0'
					i++
					for k := 1; k < 3 && i < n && isDigit(s[i]); k++ {
						val = val*8 + (s[i] - '0')
						i++
					}
					b.WriteByte(val)
				} else {
					b.WriteByte(s[i])
					i++
				}
			}
			continue
		}
		if c == '(' {
			depth++
			i++
			continue
		}
		if c == ')' {
			depth--
			i++
			if depth == 0 {
				break
			}
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), i
}

// readHex reads a hex string <...> until '>'; whitespace ignored, trailing odd
// digit padded with 0.
func readHex(s string, i int) (string, int) {
	var b strings.Builder
	hi, have := byte(0), false
	n := len(s)
	for i < n {
		c := s[i]
		if c == '>' {
			i++
			break
		}
		if !isSpace(c) {
			if v, ok := hexVal(c); ok {
				if !have {
					hi = v
					have = true
				} else {
					b.WriteByte(hi<<4 | v)
					have = false
				}
			}
		}
		i++
	}
	if have {
		b.WriteByte(hi << 4)
	}
	return b.String(), i
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// readArray reads a TJ array until ']'; elements are strings or numbers.
func readArray(s string, i int) ([]arrEl, int) {
	var elems []arrEl
	n := len(s)
	for i < n {
		c := s[i]
		if isSpace(c) {
			i++
			continue
		}
		if c == ']' {
			i++
			break
		}
		if c == '(' {
			str, next := readLiteral(s, i+1)
			elems = append(elems, arrEl{isStr: true, str: str})
			i = next
			continue
		}
		if c == '<' && (i+1 >= n || s[i+1] != '<') {
			str, next := readHex(s, i+1)
			elems = append(elems, arrEl{isStr: true, str: str})
			i = next
			continue
		}
		if isNumStart(c) {
			num, next := readNumberToken(s, i)
			elems = append(elems, arrEl{num: num})
			i = next
			continue
		}
		i++
	}
	return elems, i
}

func approxWidth(s string, fontSize, tScale float64) float64 {
	if tScale == 0 {
		tScale = 100
	}
	return float64(utf8.RuneCountInString(s)) * 0.5 * fontSize * (tScale / 100)
}

func allNum(toks []token) bool {
	for _, t := range toks {
		if t.kind != tNum {
			return false
		}
	}
	return true
}

func mat6(toks []token) mat {
	return mat{a: toks[0].num, b: toks[1].num, c: toks[2].num, d: toks[3].num, e: toks[4].num, f: toks[5].num}
}

// parsePositionedText parses a decompressed PDF content stream into positioned
// text fragments AND a doc-ordered flat string the fragments index by
// construction. Never panics — a deferred recover returns what was parsed with
// ambiguous=true; callers then fall back to extractShowText (never worse than
// today). spec 031 T004.
//
// Implements the ISO 32000-1 §9.4 text operators (BT/ET, Tm, Tf, Td, TD, T*,
// TL, Tz, Tj, TJ). The TJ numeric displacement is -(n/1000)*(Tz/100) — NO
// fontSize factor (§9.4.4; design-verdict critical fix #2).
func parsePositionedText(content string) (frags []positionedText, flat string, ambiguous bool) {
	defer func() {
		if r := recover(); r != nil {
			ambiguous = true
		}
	}()
	toks := tokenize(content)
	var flatB strings.Builder
	tm, tlm := identityMat(), identityMat()
	fontName := ""
	fontSize, leading, tScale := 0.0, 0.0, 100.0
	var stack []token
	emit := func(text string, x, y float64) {
		if text == "" || tm.rotated() {
			return
		}
		start := flatB.Len()
		flatB.WriteString(text)
		end := flatB.Len()
		flatB.WriteByte(' ')
		frags = append(frags, positionedText{
			Text: text, X: x, Y: y, FontSize: fontSize, Font: fontName,
			ByteStart: start, ByteEnd: end,
		})
	}
	pop2num := func() (float64, float64, bool) {
		if len(stack) >= 2 && stack[len(stack)-2].kind == tNum && stack[len(stack)-1].kind == tNum {
			return stack[len(stack)-2].num, stack[len(stack)-1].num, true
		}
		ambiguous = true
		return 0, 0, false
	}
	for _, tk := range toks {
		if tk.kind != tOp {
			stack = append(stack, tk)
			if len(stack) > 64 {
				stack = stack[len(stack)-64:]
			}
			continue
		}
		// ' (0x27) and " (0x22): move-to-next-line-and-show (spec §9.4.3).
		// Previously dropped (hit default); now handled as T* + Tj (FU-6 #7).
		if len(tk.str) == 1 && (tk.str[0] == 0x27 || tk.str[0] == 0x22) {
			tlm = tlm.translate(0, -leading)
			tm = tlm
			if len(stack) >= 1 && stack[len(stack)-1].kind == tStr {
				t := stack[len(stack)-1].str
				emit(t, tm.e, tm.f)
				tm.e += approxWidth(t, fontSize, tScale)
			}
			stack = stack[:0]
			continue
		}
		switch tk.str {
		case "BT":
			tm, tlm = identityMat(), identityMat()
			stack = stack[:0]
		case "Tm":
			if len(stack) >= 6 && allNum(stack[len(stack)-6:]) {
				m := mat6(stack[len(stack)-6:])
				tm, tlm = m, m
			} else {
				ambiguous = true
			}
			stack = stack[:0]
		case "Tf":
			if len(stack) >= 2 {
				if nm := stack[len(stack)-2]; nm.kind == tName {
					fontName = nm.str
				}
				if sz := stack[len(stack)-1]; sz.kind == tNum {
					fontSize = sz.num
				}
			}
			stack = stack[:0]
		case "Td":
			if tx, ty, ok := pop2num(); ok {
				tlm = tlm.translate(tx, ty)
				tm = tlm
			}
			stack = stack[:0]
		case "TD":
			if tx, ty, ok := pop2num(); ok {
				leading = -ty
				tlm = tlm.translate(tx, ty)
				tm = tlm
			}
			stack = stack[:0]
		case "T*":
			tlm = tlm.translate(0, -leading)
			tm = tlm
			stack = stack[:0]
		case "TL":
			if len(stack) >= 1 && stack[len(stack)-1].kind == tNum {
				leading = stack[len(stack)-1].num
			}
			stack = stack[:0]
		case "Tz":
			if len(stack) >= 1 && stack[len(stack)-1].kind == tNum {
				tScale = stack[len(stack)-1].num
			}
			stack = stack[:0]
		case "Tj":
			if len(stack) >= 1 && stack[len(stack)-1].kind == tStr {
				t := stack[len(stack)-1].str
				emit(t, tm.e, tm.f)
				tm.e += approxWidth(t, fontSize, tScale)
			}
			stack = stack[:0]
		case "TJ":
			if len(stack) >= 1 && stack[len(stack)-1].kind == tArr {
				startX, startY := tm.e, tm.f // the cell's origin (first string start)
				var sb strings.Builder
				for _, el := range stack[len(stack)-1].elems {
					if el.isStr {
						sb.WriteString(el.str)
					} else {
						// §9.4.4: thousandths of unscaled text space; NO fontSize.
						tm.e += -(el.num / 1000) * (tScale / 100)
					}
				}
				emit(sb.String(), startX, startY)
				tm.e += approxWidth(sb.String(), fontSize, tScale)
			}
			stack = stack[:0]
		default:
			// graphics-state / path / color / image / unknown (incl. ' and "):
			// reset the operand stack, do not touch text state. (' and " are rare
			// move-and-show operators; their text is not emitted in v1.)
			stack = stack[:0]
		}
	}
	return frags, flatB.String(), ambiguous
}
