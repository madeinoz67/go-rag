package chunk

import (
	"strings"
	"testing"
)

func TestSplit_SingleSmallChunk(t *testing.T) {
	s := NewSplitter(512, 50)
	segs := s.Split("a short sentence with a few words")
	if len(segs) != 1 {
		t.Fatalf("short input -> 1 segment, got %d", len(segs))
	}
	if segs[0].TokenCount == 0 {
		t.Error("token count must be > 0")
	}
}

func TestSplit_CountScalesWithLength(t *testing.T) {
	s := NewSplitter(512, 50)
	small := strings.Repeat("word ", 100)
	large := strings.Repeat("word ", 5000)
	if n1, n2 := len(s.Split(small)), len(s.Split(large)); n2 <= n1 {
		t.Fatalf("larger input must yield more chunks: small=%d large=%d", n1, n2)
	}
	if len(s.Split(large)) < 2 {
		t.Fatal("large input must produce multiple chunks")
	}
}

func TestSplit_OverlapBetweenChunks(t *testing.T) {
	s := NewSplitter(20, 10) // tiny sizes force multiple chunks with overlap
	text := strings.Repeat("alpha beta gamma delta epsilon ", 40)
	segs := s.Split(text)
	if len(segs) < 2 {
		t.Skip("not enough chunks to test overlap")
	}
	// Overlapping char ranges: chunk i ends after chunk i+1 starts.
	for i := 0; i < len(segs)-1; i++ {
		if segs[i].EndCharIdx <= segs[i+1].StartCharIdx {
			t.Fatalf("chunks %d/%d do not overlap: end=%d nextStart=%d", i, i+1, segs[i].EndCharIdx, segs[i+1].StartCharIdx)
		}
	}
}

func TestSplit_NoSubMinimumTail(t *testing.T) {
	s := NewSplitter(512, 50)
	s.MinTokens = 50
	// Build text large enough to chunk, with a short trailing run.
	text := strings.Repeat("tokenword ", 2000) + "tiny tail"
	segs := s.Split(text)
	if len(segs) < 2 {
		t.Skip("not enough chunks")
	}
	last := segs[len(segs)-1]
	if last.TokenCount < s.MinTokens {
		t.Fatalf("final chunk below minimum (%d < %d); should have merged", last.TokenCount, s.MinTokens)
	}
}

func TestSplit_EmptyInput(t *testing.T) {
	s := NewSplitter(512, 50)
	if segs := s.Split(""); segs != nil {
		t.Fatalf("empty input -> nil, got %v", segs)
	}
	if segs := s.Split("   \n  "); segs != nil {
		t.Fatalf("whitespace-only -> nil, got %v", segs)
	}
}
