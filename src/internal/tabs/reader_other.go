//go:build !darwin

package tabs

import (
	"context"
	"errors"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// Mac is the platform's tab reader: on Linux and Windows every browser is read from its session files. (The name is
// macOS's; NewMac is what the server asks every system for.)
type Mac struct{ Sessions }

func NewMac() *Mac { return &Mac{Sessions{Dirs: SessionDirs}} }

func (m *Mac) Read(ctx context.Context, b browsers.Browser) Browser { return m.Sessions.Read(ctx, b) }

func (*Mac) Close(context.Context, string, []CloseItem) (int, []string, error) {
	return 0, nil, errors.New("closing tabs here needs the Browser Foreman extension in that browser")
}
