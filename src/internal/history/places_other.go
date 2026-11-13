//go:build !darwin && !linux && !windows

package history

// SettingsURL is empty: there is no Full Disk Access to grant here.
const SettingsURL = ""

func places() []place { return nil }
