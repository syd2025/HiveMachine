// Package wallet provides cryptographic wallet management for HiveMachine.
// wordlist.go provides access to the BIP-39 2048-word English wordlist.
package wallet

import (
	"fmt"
	"os"
	"path/filepath"
)

// WordList returns the full BIP-39 English wordlist.
func WordList() [2048]string {
	return bip39WordList
}

// WordAt returns the word at index i (0-2047).
func WordAt(i int) string {
	if i < 0 || i >= 2048 {
		return ""
	}
	return bip39WordList[i]
}

// WordIndex returns the index of word, or -1 if not in the list.
func WordIndex(word string) int {
	for i, w := range bip39WordList {
		if w == word {
			return i
		}
	}
	return -1
}

// EnsureWordListFile writes the wordlist to a file for external use.
// Not required for in-process operation.
func EnsureWordListFile() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".hivemachine", "wordlist.txt")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, w := range bip39WordList {
		fmt.Fprintln(f, w)
	}
	return nil
}
