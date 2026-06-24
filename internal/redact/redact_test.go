package redact

import (
	"regexp"
	"strings"
	"testing"
)

func TestApply_AWSKey(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	in := "config key=AKIAIOSFODNN7EXAMPLE region=us-east-1"
	out, f := s.Apply(in)
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("AWS key not redacted")
	}
	if !strings.Contains(out, "[REDACTED:aws-key]") {
		t.Error("AWS key placeholder missing")
	}
	assertFinding(t, f, "aws-key", 1)
}

func TestApply_GitHubToken(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	in := "GITHUB_TOKEN=ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789"
	out, f := s.Apply(in)
	if strings.Contains(out, "ghp_") {
		t.Error("GitHub token not redacted")
	}
	assertFinding(t, f, "github-token", 1)
}

func TestApply_Email(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	in := "contact ops@example.com for details"
	out, f := s.Apply(in)
	if strings.Contains(out, "ops@example.com") {
		t.Error("email not redacted")
	}
	assertFinding(t, f, "email", 1)
}

func TestApply_PrivateKey(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	in := "key:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----\n"
	out, f := s.Apply(in)
	if strings.Contains(out, "MIIEowIBAA") {
		t.Error("private key body not redacted")
	}
	assertFinding(t, f, "private-key", 1)
}

func TestApply_SSN(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	in := "SSN: 123-45-6789"
	out, f := s.Apply(in)
	if strings.Contains(out, "123-45-6789") {
		t.Error("SSN not redacted")
	}
	assertFinding(t, f, "ssn", 1)
}

func TestApply_CreditCard_LUHN(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	// 4111 1111 1111 1111 is a valid LUHN test card
	in := "card: 4111 1111 1111 1111 expires 12/25"
	out, f := s.Apply(in)
	if strings.Contains(out, "4111") {
		t.Error("valid LUHN card not redacted")
	}
	assertFinding(t, f, "credit-card", 1)
}

func TestApply_CreditCard_NonLUHN_NotRedacted(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	// 13 digits that FAIL LUHN — should NOT be redacted (false-positive guard)
	in := "ref: 1234 5678 9012 3"
	out, f := s.Apply(in)
	if !strings.Contains(out, "1234 5678 9012 3") {
		t.Error("non-LUHN digit string was incorrectly redacted")
	}
	for _, find := range f {
		if find.Type == "credit-card" {
			t.Error("non-LUHN should not produce a credit-card finding")
		}
	}
}

func TestApply_CustomPatterns(t *testing.T) {
	custom := []Pattern{{Type: "cn-id", re: nil}} // will compile below
	custom[0].re = regexpCompile(t, `\d{17}[\dXx]`)
	s := NewScanner(append(DefaultPatterns(nil), custom...))
	in := "id: 110101199001011234 done"
	out, f := s.Apply(in)
	if strings.Contains(out, "110101199001011234") {
		t.Error("custom CN-ID pattern not redacted")
	}
	assertFinding(t, f, "cn-id", 1)
}

func TestApply_CleanText_Unchanged(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	in := "a clean document about retrieval search and ranking"
	out, f := s.Apply(in)
	if out != in {
		t.Error("clean text was modified (should be unchanged)")
	}
	if len(f) != 0 {
		t.Errorf("clean text produced findings: %+v", f)
	}
}

func TestApply_NilScanner_NoOp(t *testing.T) {
	var s *Scanner
	out, f := s.Apply("anything")
	if out != "anything" || f != nil {
		t.Error("nil scanner should be a no-op")
	}
}

func assertFinding(t *testing.T, findings []Finding, typ string, want int) {
	t.Helper()
	for _, f := range findings {
		if f.Type == typ {
			if f.Count != want {
				t.Errorf("finding %s: count=%d, want %d", typ, f.Count, want)
			}
			return
		}
	}
	t.Errorf("finding %s not present in %+v", typ, findings)
}

func regexpCompile(t *testing.T, pat string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(pat)
	if err != nil {
		t.Fatal(err)
	}
	return re
}

// TestApplyWithEdits_AndTranslateOffset: the offset-aware pass returns one Edit per
// substitution in original-text coordinates, and TranslateOffset maps a pre-redaction
// offset into redacted-text space (audit H23/spec 025, research R3).
func TestApplyWithEdits_AndTranslateOffset(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	in := "contact ops@example.com for details"
	red, findings, edits := s.ApplyWithEdits(in)
	if !strings.Contains(red, "[REDACTED:email]") {
		t.Error("email placeholder missing")
	}
	assertFinding(t, findings, "email", 1)
	if len(edits) != 1 {
		t.Fatalf("want 1 edit, got %d: %+v", len(edits), edits)
	}
	e := edits[0]
	if e.Pos != strings.Index(in, "ops@example.com") {
		t.Errorf("edit Pos=%d want %d", e.Pos, strings.Index(in, "ops@example.com"))
	}
	if e.RemovedLen != len("ops@example.com") || e.InsertedLen != len("[REDACTED:email]") {
		t.Errorf("edit lengths: removed=%d inserted=%d", e.RemovedLen, e.InsertedLen)
	}
	// Offset before the substitution is unchanged.
	if got := TranslateOffset(0, edits); got != 0 {
		t.Errorf("TranslateOffset(0)=%d want 0", got)
	}
	// Offset at the match start maps to the placeholder start (unchanged).
	if got := TranslateOffset(e.Pos, edits); got != e.Pos {
		t.Errorf("TranslateOffset(match start)=%d want %d", got, e.Pos)
	}
	// End-of-original maps to end-of-redacted (shifted by the net delta).
	if got := TranslateOffset(len(in), edits); got != len(red) {
		t.Errorf("TranslateOffset(end)=%d want len(redacted)=%d", got, len(red))
	}
}

// TestApplyWithEdits_MatchesApply: the offset-aware pass produces byte-identical
// redacted text to the canonical Apply (one redaction code path, privacy invariant).
func TestApplyWithEdits_MatchesApply(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	in := "key AKIAIOSFODNN7EXAMPLE mail ops@example.com card 4111 1111 1111 1111 end"
	a, _ := s.Apply(in)
	w, _, _ := s.ApplyWithEdits(in)
	if a != w {
		t.Errorf("redacted text differs:\nApply:         %q\nApplyWithEdits:%q", a, w)
	}
}

// TestApplyWithEdits_LUHNCardOffset: a redacted LUHN-valid card emits an edit whose
// delta is reflected by TranslateOffset for offsets after it.
func TestApplyWithEdits_LUHNCardOffset(t *testing.T) {
	s := NewScanner(DefaultPatterns(nil))
	card := "4111 1111 1111 1111"
	in := "card: " + card + " tail"
	red, _, edits := s.ApplyWithEdits(in)
	if !strings.Contains(red, "[REDACTED:credit-card]") {
		t.Errorf("LUHN card not redacted: %q", red)
	}
	if len(edits) != 1 {
		t.Fatalf("want 1 edit, got %+v", edits)
	}
	e := edits[0]
	// The CC regex consumes optional trailing separators, so the match may be a byte
	// or two longer than the bare digit run — assert it covers the card digits.
	if e.RemovedLen < len(card) {
		t.Errorf("removed len=%d shorter than card digits %d", e.RemovedLen, len(card))
	}
	// "tail" sits after the card in both spaces; translation must map one to the other.
	tailOrig := strings.Index(in, "tail")
	tailRed := strings.Index(red, "tail")
	if got := TranslateOffset(tailOrig, edits); got != tailRed {
		t.Errorf("TranslateOffset(tail)=%d want %d", got, tailRed)
	}
}

// TestTranslateOffset_Identity: no edits → identity (redaction disabled / clean text).
func TestTranslateOffset_Identity(t *testing.T) {
	for _, off := range []int{0, 5, 100} {
		if got := TranslateOffset(off, nil); got != off {
			t.Errorf("TranslateOffset(%d, nil)=%d want %d", off, got, off)
		}
	}
	s := NewScanner(DefaultPatterns(nil))
	_, _, edits := s.ApplyWithEdits("a clean document about search")
	if len(edits) != 0 {
		t.Errorf("clean text produced edits: %+v", edits)
	}
}
