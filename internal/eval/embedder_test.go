package eval

import (
	"context"
	"testing"
)

func TestDeterministicEmbedder_Reproducible(t *testing.T) {
	d := NewDeterministicEmbedder()
	a, err := d.Embed(context.Background(), []string{"how does chunking work"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	b, _ := d.Embed(context.Background(), []string{"how does chunking work"})
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("expected 1 vector each, got %d / %d", len(a), len(b))
	}
	if len(a[0]) != detDimensions {
		t.Fatalf("dim = %d, want %d", len(a[0]), detDimensions)
	}
	// Identical input → byte-identical vector (reproducibility, SC-004).
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Fatalf("vectors differ at dim %d: %v vs %v", i, a[0][i], b[0][i])
		}
	}
}

func TestDeterministicEmbedder_ContentSensitive(t *testing.T) {
	d := NewDeterministicEmbedder()
	v, _ := d.Embed(context.Background(), []string{"chunking documents into pieces", "chunking documents into pieces too"})
	if cosine(v[0], v[1]) < 0.8 {
		t.Fatalf("near-identical texts should have high cosine, got %f", cosine(v[0], v[1]))
	}
	diff, _ := d.Embed(context.Background(), []string{"authentication tokens", "the quick brown fox"})
	if cosine(diff[0], diff[1]) > 0.9 {
		t.Fatalf("unrelated texts should have low cosine, got %f", cosine(diff[0], diff[1]))
	}
}

func TestDeterministicEmbedder_MetaData(t *testing.T) {
	d := NewDeterministicEmbedder()
	if d.Dimensions() != detDimensions {
		t.Fatalf("Dimensions = %d, want %d", d.Dimensions(), detDimensions)
	}
	if d.Model() != "deterministic-hash" {
		t.Fatalf("Model = %q", d.Model())
	}
}

func TestDeterministicEmbedder_Normalized(t *testing.T) {
	d := NewDeterministicEmbedder()
	v, _ := d.Embed(context.Background(), []string{"a reasonably long sentence with several distinct tokens inside"})
	// L2 norm of a normalized vector is ~1.
	var sum float64
	for _, x := range v[0] {
		sum += float64(x) * float64(x)
	}
	if sum < 0.99 || sum > 1.01 {
		t.Fatalf("vector not L2-normalized (||v||^2 = %f)", sum)
	}
}

func cosine(a, b []float32) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot // both inputs are L2-normalized → dot product == cosine
}
