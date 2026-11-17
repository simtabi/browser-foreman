package tabs

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// profilesIn lists a browser's profile folders under its data folder: Chromium's Default and "Profile N", or every
// Firefox profile (the * in glob).
func profilesIn(globs ...string) []string {
	var out []string
	for _, g := range globs {
		m, _ := filepath.Glob(g)
		for _, p := range m {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// SessionDirs is where this system's browsers keep the profiles Sessions reads.
func SessionDirs(b browsers.Browser) []string { return sessionDirs(b) }
