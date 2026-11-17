package api

import (
	"context"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// livePins are the extension pins whose key still exists: a pin whose key was revoked (disconnected) pins nothing.
func (s *Server) livePins(ctx context.Context) (map[string]store.ExtensionPin, error) {
	p, err := store.LoadPrefs(ctx, s.Data)
	if err != nil {
		return nil, err
	}
	all, err := s.Data.Tokens(ctx, "")
	if err != nil {
		return nil, err
	}
	alive := map[string]bool{}
	for _, t := range all {
		alive[t.ID] = true
	}
	out := map[string]store.ExtensionPin{}
	for b, pin := range p.ExtensionPins {
		if alive[pin.Token] {
			out[b] = pin
		}
	}
	return out, nil
}

// pinExtension makes the copy just approved the one connected copy of its browser: it records the pin and revokes
// every other key for that browser name, so a second copy (another Chrome on this computer calling itself "Google
// Chrome") stops instead of racing it for one identity. The revoked copy is told 401 and stops asking by itself.
func (s *Server) pinExtension(ctx context.Context, browser, instance, version, token string) (revoked int, err error) {
	all, err := s.Data.Tokens(ctx, "")
	if err != nil {
		return 0, err
	}
	for _, t := range all {
		if t.Name == extPrefix+browser && t.ID != token {
			if err := s.Data.RevokeToken(ctx, t.ID); err != nil {
				return revoked, err
			}
			revoked++
		}
	}
	_, err = store.UpdatePrefs(ctx, s.Data, func(p *store.Prefs) error {
		if p.ExtensionPins == nil {
			p.ExtensionPins = map[string]store.ExtensionPin{}
		}
		p.ExtensionPins[browser] = store.ExtensionPin{Instance: instance, Token: token, Version: version, At: s.Now().UTC()}
		return nil
	})
	return revoked, err
}

// shortInstance is the first four characters of an instance id, enough to tell two copies apart on screen.
func shortInstance(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 4 {
		return id[:4]
	}
	return id
}
