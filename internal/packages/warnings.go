package packages

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	accountRead     = regexp.MustCompile(`\bdevmachine_account\b`)
	accountRegister = regexp.MustCompile(`^\s*register:\s*["']?devmachine_account["']?\s*$`)
)

const borrowedAccount = "reads devmachine_account, but no task in this package registers it. " +
	"A registered variable outlives the role that set it, so this package reads the account the " +
	"previous package left, which can be another workspace's. Add the \"Read the account's home and group\" " +
	"task as the first task: ansible.builtin.user with name: \"{{ devmachine_workspace.user }}\", " +
	"check_mode: true, changed_when: false and register: devmachine_account, as the upstream packages do"

// Warnings returns what in the package in dir is likely wrong but does not
// stop it from running. Validation still passes with warnings.
func Warnings(dir string) ([]Problem, error) {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}
	var (
		firstRead  *Problem
		registered bool
	)
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		switch filepath.Ext(path) {
		case ".yml", ".yaml", ".j2":
		default:
			return nil
		}
		if filepath.Base(path) == FileName {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for line := 1; scanner.Scan(); line++ {
			text := scanner.Text()
			switch {
			case strings.HasPrefix(strings.TrimSpace(text), "#"):
			case accountRegister.MatchString(text):
				registered = true
			case firstRead == nil && accountRead.MatchString(text):
				firstRead = &Problem{File: relative, Line: line, What: borrowedAccount}
			}
		}
		return scanner.Err()
	})
	if err != nil {
		return nil, err
	}
	if firstRead == nil || registered {
		return nil, nil
	}
	return []Problem{*firstRead}, nil
}
