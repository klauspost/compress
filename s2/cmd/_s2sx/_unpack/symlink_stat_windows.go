//go:build windows

package main

func isIncompleteSymlinkResolution(err error) bool {
	return false
}
