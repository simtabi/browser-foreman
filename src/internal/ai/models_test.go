package ai

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestOpenAIModelsKeepsChatModelsOnly(t *testing.T) {
	key := rand.Text() // generated, never a real key
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":"text-embedding-3-large"},{"id":"tts-1"},{"id":"dall-e-3"},{"id":"o4-mini"},{"id":"whisper-1"}]}`))
	}))
	defer srv.Close()
	got, err := (&OpenAI{URL: srv.URL + "/v1", Key: key}).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []Model{{ID: "gpt-5"}, {ID: "o4-mini"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if _, err := (&OpenAI{URL: srv.URL + "/v1"}).Models(context.Background()); err == nil {
		t.Fatal("listed without a key")
	}
}

func TestOllamaModelsListsWhatIsPulled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:3b"},{"name":"qwen3:8b"}]}`))
	}))
	defer srv.Close()
	got, err := (&Ollama{URL: srv.URL}).Models(context.Background())
	if err != nil || !reflect.DeepEqual(got, []Model{{ID: "llama3.2:3b"}, {ID: "qwen3:8b"}}) {
		t.Fatalf("%v %+v", err, got)
	}
	srv.Close()
	if _, err := (&Ollama{URL: srv.URL}).Models(context.Background()); err == nil {
		t.Fatal("a stopped Ollama answered")
	}
}
