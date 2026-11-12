//go:build !darwin

package tabs

import (
	"context"
	"errors"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// Mac is only real on macOS; elsewhere it reads nothing, so the server still runs.
type Mac struct{}

func NewMac() *Mac { return &Mac{} }

func (*Mac) Read(_ context.Context, b browsers.Browser) Browser {
	return Browser{Browser: b.Name, Error: "reading tabs is supported on macOS so far"}
}

func (*Mac) Close(context.Context, string, []CloseItem) (int, []string, error) {
	return 0, nil, errors.New("closing tabs is supported on macOS so far")
}
