package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/webhooks"
)

func TestDeliveryLog(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(418) }))
	defer receiver.Close()
	s := newServer(t, apiTok)
	s.Deliveries = webhooks.NewLog(t.TempDir())
	s.Dispatch.Log = func(d webhooks.Delivery) { _ = s.Deliveries.Add(d) }
	h := s.Handler()
	w := send(h, "POST", "/api/v1/webhooks", `{"url":"`+receiver.URL+`/hook","events":["*"],"secret":"`+hookSecret+`"}`)
	var hook struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &hook)
	if w.Code != 201 || hook.ID == "" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/webhooks/"+hook.ID+"/test", ""); w.Code != 200 {
		t.Fatalf("test %d", w.Code)
	}
	var out struct{ Deliveries []webhooks.Delivery }
	_ = json.Unmarshal(send(h, "GET", "/api/v1/webhooks/deliveries?subscription="+hook.ID, "").Body.Bytes(), &out)
	if len(out.Deliveries) != 1 || out.Deliveries[0].Status != 418 || out.Deliveries[0].OK || out.Deliveries[0].Type != "webhook.test" {
		t.Fatalf("%+v", out.Deliveries)
	}
	if w := send(h, "GET", "/api/v1/webhooks/deliveries?limit=0", ""); w.Code != 400 {
		t.Fatalf("limit 0: %d", w.Code)
	}
	if strings.Contains(send(h, "GET", "/api/v1/webhooks/deliveries", "").Body.String(), hookSecret) {
		t.Fatal("a secret reached the delivery log")
	}
}
