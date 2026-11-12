# Receive a webhook

Check the `X-Brofo-Signature` header before you trust a delivery: recompute the HMAC over `<t>.<raw body>` with your
secret, compare in constant time, and refuse a `t` more than five minutes old.

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func verified(secret string, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, false
	}
	var ts, sig string
	for _, part := range strings.Split(r.Header.Get("X-Brofo-Signature"), ",") {
		k, v, _ := strings.Cut(part, "=")
		if k == "t" {
			ts = v
		} else if k == "v1" {
			sig = v
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(sec, 0)).Abs() > 5*time.Minute {
		return nil, false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return body, hmac.Equal([]byte(hex.EncodeToString(m.Sum(nil))), []byte(sig))
}

func main() {
	secret := os.Getenv("BROFO_WEBHOOK_SECRET")
	http.HandleFunc("/brofo", func(w http.ResponseWriter, r *http.Request) {
		body, ok := verified(secret, r)
		if !ok {
			http.Error(w, "bad signature", http.StatusUnauthorized) // a 4xx is not retried
			return
		}
		_ = body // handle the event
		w.WriteHeader(http.StatusNoContent)
	})
	_ = http.ListenAndServe("127.0.0.1:9000", nil)
}
```

The full delivery format is in [webhooks](../tools/webhooks.md).

---
[← Docs index](../../README.md#documentation)
