//go:build !darwin && !linux && !windows

package service

// Install is unsupported here.
func (Manager) Install(Spec) (string, error) { return "", ErrUnsupported }

// Uninstall is unsupported here.
func (Manager) Uninstall() (bool, error) { return false, ErrUnsupported }

// Status reports nothing installed.
func (Manager) Status() Status { return Status{Detail: ErrUnsupported.Error()} }
