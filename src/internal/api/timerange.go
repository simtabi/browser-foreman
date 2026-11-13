package api

import (
	"errors"
	"net/url"
	"time"
)

// timeRange reads ?from= and ?to=, the custom range every period picker can send: RFC 3339, or the page's
// datetime-local form ("2006-01-02T15:04", in this computer's time zone). Either may be missing (unbounded);
// both missing answers set false, and the caller falls back to ?days=.
func timeRange(q url.Values) (from, to time.Time, set bool, err error) {
	parse := func(name string) (time.Time, error) {
		v := q.Get(name)
		if v == "" {
			return time.Time{}, nil
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
		if t, err := time.ParseInLocation("2006-01-02T15:04", v, time.Local); err == nil {
			if name == "to" { // to the minute: the whole minute counts, so "to 09:30" keeps a visit at 09:30:40
				return t.Add(time.Minute - time.Nanosecond), nil
			}
			return t, nil
		}
		return time.Time{}, errors.New(name + ": a date and time like 2026-11-13T09:30")
	}
	if from, err = parse("from"); err != nil {
		return
	}
	if to, err = parse("to"); err != nil {
		return
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return from, to, false, errors.New("to is before from")
	}
	return from, to, !from.IsZero() || !to.IsZero(), nil
}
