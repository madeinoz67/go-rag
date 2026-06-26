package caption

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCaption_Factory (spec 031 FU-1): the factory dispatches to the right provider.
func TestCaption_Factory(t *testing.T) {
	if _, ok := New("ollama", "http://x", "m", "").(*Ollama); !ok {
		t.Error("provider 'ollama' should return *Ollama")
	}
	if _, ok := New("", "http://x", "m", "").(*Ollama); !ok {
		t.Error("empty provider should default to *Ollama")
	}
	if _, ok := New("openai", "http://x", "m", "key").(*OpenAI); !ok {
		t.Error("provider 'openai' should return *OpenAI")
	}
	if _, ok := New("OpenAI-Compatible", "http://x", "m", "key").(*OpenAI); !ok {
		t.Error("'OpenAI-Compatible' (case-insensitive) should return *OpenAI")
	}
}

// TestCaption_OpenAI (spec 031 FU-1): the OpenAI-compatible provider sends a
// multimodal chat request with a data-URL image + Bearer auth, and parses the
// response. Hermetic (httptest — no real API call).
func TestCaption_OpenAI(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %s, want /chat/completions", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "a bar chart with rising bars"}}},
		})
	}))
	defer srv.Close()

	c := NewOpenAI(srv.URL, "gpt-4o", "sk-test-key")
	caption, err := c.Caption(context.Background(), []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}, "page 1, jpg")
	if err != nil {
		t.Fatalf("Caption: %v", err)
	}
	if caption != "a bar chart with rising bars" {
		t.Errorf("caption: got %q, want %q", caption, "a bar chart with rising bars")
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization: got %q, want Bearer sk-test-key", gotAuth)
	}
	// Verify the request had a multimodal image_url content part.
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: got %d, want 1", len(msgs))
	}
	msg, _ := msgs[0].(map[string]any)
	content, _ := msg["content"].([]any)
	hasImageURL := false
	for _, p := range content {
		if pm, ok := p.(map[string]any); ok && pm["type"] == "image_url" {
			hasImageURL = true
			iu, _ := pm["image_url"].(map[string]any)
			url, _ := iu["url"].(string)
			if url == "" || url[:5] != "data:" {
				t.Errorf("image_url should be a data-URL; got %q...", url[:min(20, len(url))])
			}
		}
	}
	if !hasImageURL {
		t.Error("expected an image_url content part in the request")
	}
}

// TestCaption_OpenAI_PermanentError (spec 031 FU-1): a 4xx response is a permanent
// failure (WrapPermanent), not retried.
func TestCaption_OpenAI_PermanentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad model"}`))
	}))
	defer srv.Close()
	c := NewOpenAI(srv.URL, "bad-model", "key")
	_, err := c.Caption(context.Background(), []byte{0xFF, 0xD8, 0xFF}, "")
	if !IsPermanent(err) {
		t.Errorf("4xx should be permanent; got %v", err)
	}
}

// TestCaption_OpenAI_EmptyImage: empty bytes → ErrNothingToCaption.
func TestCaption_OpenAI_EmptyImage(t *testing.T) {
	c := NewOpenAI("http://x", "m", "")
	_, err := c.Caption(context.Background(), nil, "")
	if !IsNothing(err) {
		t.Errorf("empty image should be ErrNothingToCaption; got %v", err)
	}
}
