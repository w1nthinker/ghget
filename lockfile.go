package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
)

const lockName = ".ghget.lock"

// Entry is one vendored dest in the lockfile.
type Entry struct {
	URL       string `json:"url"`
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	Ref       string `json:"ref"`
	Path      string `json:"path"`
	Dest      string `json:"dest"`
	Type      string `json:"type"`
	Commit    string `json:"commit"`
	FetchedAt string `json:"fetchedAt"`
}

// Lock maps dest -> Entry. JSON-marshaling a map sorts keys, which keeps
// the file diffable.
type Lock map[string]Entry

func readLock() (Lock, error) {
	data, err := os.ReadFile(lockName)
	if errors.Is(err, fs.ErrNotExist) {
		return Lock{}, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return l, nil
}

func writeLock(l Lock) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockName, append(data, '\n'), 0o644)
}

func sortedEntries(l Lock) []Entry {
	dests := make([]string, 0, len(l))
	for d := range l {
		dests = append(dests, d)
	}
	sort.Strings(dests)
	out := make([]Entry, len(dests))
	for i, d := range dests {
		out[i] = l[d]
	}
	return out
}
