//go:build !darwin && !linux && !windows

package tabs

import "github.com/simtabi/browser-foreman/src/internal/browsers"

func sessionDirs(browsers.Browser) []string { return nil }
