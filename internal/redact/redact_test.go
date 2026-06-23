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
