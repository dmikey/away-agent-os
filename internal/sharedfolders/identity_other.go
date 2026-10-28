//go:build !darwin && !linux

package sharedfolders

import "os"

func fileIdentity(info os.FileInfo) string { return info.Name() }
