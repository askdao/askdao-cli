package recommender

import (
	"io/fs"
	"os"
)

// defaultStat / readDir wrap os calls so callers in policy.go don't pull os
// directly. Keeps the file's import surface small + lets tests in this package
// stub osStat where needed.
func defaultStat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func readDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}
