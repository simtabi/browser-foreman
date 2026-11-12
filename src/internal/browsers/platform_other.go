//go:build !darwin

package browsers

import (
	"context"
	"errors"
)

// other is every system not supported yet: no browsers found, nothing opened.
type other struct{}

func NewPlatform() Platform         { return other{} }
func (other) Installed() []Browser  { return nil }
func (other) DefaultBundle() string { return "" }
func (other) Icon(Browser, int) ([]byte, error) {
	return nil, errors.New("no icons on this system yet")
}
func (other) Launch(context.Context, []string) error { return ErrUnsupported }
