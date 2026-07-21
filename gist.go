package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gistInfo returns a gist's latest revision SHA and its filename -> raw_url
// map (raw URLs are already pinned to that revision).
func gistInfo(id string) (string, map[string]string, error) {
	resp, err := apiGet(apiBase+"/gists/"+id, "application/vnd.github+json")
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	var g struct {
		History []struct {
			Version string `json:"version"`
		} `json:"history"`
		Files map[string]struct {
			RawURL string `json:"raw_url"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return "", nil, err
	}
	if len(g.History) == 0 {
		return "", nil, fmt.Errorf("gist %s has no revisions", id)
	}
	files := make(map[string]string, len(g.Files))
	for name, f := range g.Files {
		files[name] = f.RawURL
	}
	return g.History[0].Version, files, nil
}

// anchorize mimics GitHub's gist file anchors: "file-" + lowercased
// filename with every character outside [a-z0-9_] as "-".
func anchorize(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return "file-" + b.String()
}

// matchGistFile resolves a #file-... URL fragment (or an exact filename)
// to a real gist filename.
// ponytail: anchors are compared with dashes stripped so GitHub's exact
// dash-collapsing rules don't matter; ambiguous only for pathological names.
func matchGistFile(files map[string]string, frag string) (string, bool) {
	want := strings.ReplaceAll(strings.ToLower(frag), "-", "")
	for name := range files {
		if name == frag || strings.ReplaceAll(anchorize(name), "-", "") == want {
			return name, true
		}
	}
	return "", false
}

// downloadGist writes one gist file (file != "") to dest, or every file
// into the dest directory.
func downloadGist(files map[string]string, file, dest string) error {
	if file != "" {
		raw, ok := files[file]
		if !ok {
			return fmt.Errorf("file %q not in gist", file)
		}
		return saveURL(raw, dest)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for name, raw := range files {
		if err := saveURL(raw, filepath.Join(dest, name)); err != nil {
			return err
		}
	}
	return nil
}
