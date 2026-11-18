package muninn

import (
	"context"
	"math"
	"strings"
	"testing"
)

// TestActivate_LimitOutOfRange is the code-scanning #1 guard: a limit outside
// int32 must be refused before it truncates into ActivateRequest.MaxResults.
func TestActivate_LimitOutOfRange(t *testing.T) {
	g := &grpcClient{}
	for _, limit := range []int{math.MaxInt32 + 1, math.MinInt32 - 1} {
		_, err := g.Activate(context.Background(), "default", []string{"x"}, limit)
		if err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Fatalf("Activate(limit=%d) err = %v, want out-of-range refusal", limit, err)
		}
	}
}
