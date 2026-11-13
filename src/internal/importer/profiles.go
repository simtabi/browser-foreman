package importer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// fileNames and fileExts are the only files ReadFile opens, so it cannot be used to read anything else on the computer.
var (
	fileNames = []string{"Bookmarks", "places.sqlite", "Bookmarks.plist"}
	fileExts  = []string{".html", ".htm", ".json", ".plist", ".sqlite"}
)

// ErrNotBookmarksName is ReadFile refusing a file whose name is not a bookmarks file's.
var ErrNotBookmarksName = errors.New("only bookmarks files can be read: Bookmarks, places.sqlite, Bookmarks.plist, or a .html, .json, .plist or .sqlite file")

// ReadFile reads a bookmarks file on this computer: a browser profile's own file, or an export. Symbolic links are
// followed first and the real name checked, and a Firefox database is read from a copy.
func ReadFile(path string) (store.Tree, Format, error) {
	real, err := filepath.EvalSymlinks(expandHome(path))
	if err != nil {
		return store.Tree{}, "", fileError(path, err)
	}
	base := filepath.Base(real)
	ok := false
	for _, n := range fileNames {
		ok = ok || base == n
	}
	for _, e := range fileExts {
		ok = ok || strings.HasSuffix(strings.ToLower(base), e)
	}
	if !ok {
		return store.Tree{}, "", ErrNotBookmarksName
	}
	if info, err := os.Stat(real); err != nil {
		return store.Tree{}, "", fileError(path, err)
	} else if !info.Mode().IsRegular() {
		return store.Tree{}, "", fmt.Errorf("%s is not a file", path)
	}
	if base == "places.sqlite" || strings.HasSuffix(base, ".sqlite") {
		t, err := ReadFirefoxFile(real)
		return t, Firefox, fileError(path, err)
	}
	f, err := os.Open(real)
	if err != nil {
		return store.Tree{}, "", fileError(path, err)
	}
	defer f.Close()
	return Read(f)
}

func fileError(path string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrPermission) {
		if runtime.GOOS == "darwin" {
			return fmt.Errorf("macOS did not allow reading %s: give Browser Foreman Full Disk Access (System Settings › Privacy & Security), or export the bookmarks from the browser and import that file", path)
		}
		return fmt.Errorf("not allowed to read %s", path)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("no file at %s", path)
	}
	return err
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, rest)
		}
	}
	return p
}

// Profile is a browser profile's bookmarks file found on this computer.
type Profile struct {
	Browser  string `json:"browser"`
	Profile  string `json:"profile"`        // the profile's folder name
	Name     string `json:"name,omitempty"` // the name the browser shows for it, when it says
	Path     string `json:"path"`
	Readable bool   `json:"readable"`
	Why      string `json:"why,omitempty"` // why it cannot be read
}

// place is where one browser keeps its profiles' bookmarks: a glob, and for Chromium browsers the folder holding
// "Local State", which names the profiles.
type place struct {
	browser, glob, userData string
}

// FindProfiles lists the bookmarks files of every browser profile on this computer, readable or not. Nothing is
// opened beyond a one-byte read to tell whether it can be.
func FindProfiles() []Profile { return findProfiles(places()) }

func findProfiles(ps []place) []Profile {
	out := []Profile{}
	for _, p := range ps {
		matches, _ := filepath.Glob(p.glob)
		sort.Strings(matches)
		names := profileNames(p.userData)
		for _, m := range matches {
			dir := filepath.Base(filepath.Dir(m))
			pr := Profile{Browser: p.browser, Profile: dir, Name: names[dir], Path: m, Readable: true}
			if f, err := os.Open(m); err != nil {
				pr.Readable, pr.Why = false, fileError(m, err).Error()
			} else {
				if _, err := f.Read(make([]byte, 1)); err != nil {
					pr.Readable, pr.Why = false, fileError(m, err).Error()
				}
				f.Close()
			}
			out = append(out, pr)
		}
		if len(matches) == 0 { // a browser folder the system will not let this program into
			dir := p.glob
			if i := strings.Index(dir, "*"); i >= 0 {
				dir = dir[:i] + "x"
			}
			dir = filepath.Dir(dir)
			if _, err := os.ReadDir(dir); errors.Is(err, fs.ErrPermission) {
				out = append(out, Profile{Browser: p.browser, Path: p.glob, Why: fileError(dir, err).Error()})
			}
		}
	}
	return out
}

// profileNames reads a Chromium browser's Local State for the names it shows for its profile folders.
func profileNames(userData string) map[string]string {
	out := map[string]string{}
	if userData == "" {
		return out
	}
	b, err := os.ReadFile(filepath.Join(userData, "Local State"))
	if err != nil || len(b) > 32<<20 {
		return out
	}
	var ls struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if json.Unmarshal(b, &ls) == nil {
		for dir, v := range ls.Profile.InfoCache {
			out[dir] = v.Name
		}
	}
	return out
}

// chromium is a Chromium browser's profiles under its user data folder.
func chromium(browser, userData string) place {
	return place{browser, filepath.Join(userData, "*", "Bookmarks"), userData}
}
