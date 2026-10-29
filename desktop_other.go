//go:build !windows

package main

import "errors"

func runDesktop(*studio) error {
	return errors.New("this desktop build targets Windows; use -browser for the local browser interface")
}
