//go:build !windows

package main

// On non-Windows platforms there is no service control manager: the agent only
// ever runs in the foreground. These stubs let main.go stay platform-agnostic.

func inWindowsService() (bool, error) { return false, nil }
func configureServiceLogging()        {}
func runWindowsService() error        { return nil }
