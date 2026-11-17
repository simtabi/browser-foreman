package ai

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
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

func TestModelListsAskOnceMoreAfterAGatewayError(t *testing.T) {
	retryWait = 0
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "error code: 504", http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:8b"}]}`))
	}))
	defer srv.Close()
	got, err := (&Ollama{URL: srv.URL}).Models(context.Background())
	if err != nil || len(got) != 1 || calls.Load() != 2 {
		t.Fatalf("%v %+v after %d calls", err, got, calls.Load())
	}
	// A second gateway error is reported, not retried for ever.
	var down atomic.Int32
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		down.Add(1)
		http.Error(w, "error code: 504", http.StatusGatewayTimeout)
	}))
	defer dead.Close()
	if _, err := (&Ollama{URL: dead.URL}).Models(context.Background()); err == nil || down.Load() != 2 {
		t.Fatalf("%v after %d calls", err, down.Load())
	}
}
