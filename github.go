package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const apiBase = "https://api.github.com"

// token returns the auth token to use: GITHUB_TOKEN / GH_TOKEN env vars
// win, then the gh CLI's stored login if gh is installed and authed.
// Empty means unauthenticated (60 req/h rate limit).
var token = sync.OnceValue(func() string {
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	if tok := os.Getenv("GH_TOKEN"); tok != "" {
		return tok
	}
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
})

// apiGet performs a GET with auth and retries transient 5xx responses
// (GitHub throws 502s under load): up to 5 tries with linear backoff.
func apiGet(rawURL, accept string) (*http.Response, error) {
	var lastStatus string
	for try := 1; try <= 5; try++ {
		req, err := http.NewRequest("GET", rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		if tok := token(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 500 {
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				resp.Body.Close()
				return nil, fmt.Errorf("GET %s: %s: %s", rawURL, resp.Status, body)
			}
			return resp, nil
		}
		lastStatus = resp.Status
		resp.Body.Close()
		time.Sleep(time.Duration(try) * time.Second)
	}
	return nil, fmt.Errorf("GET %s: %s after 5 tries", rawURL, lastStatus)
}

// resolveCommit returns the SHA of the commit that last touched path on ref.
func resolveCommit(owner, repo, ref, path string) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/commits?sha=%s&path=%s&per_page=1",
		apiBase, owner, repo, url.QueryEscape(ref), url.QueryEscape(path))
	resp, err := apiGet(u, "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var commits []struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return "", err
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("no commits found for %s at ref %s", path, ref)
	}
	return commits[0].SHA, nil
}

func contentsURL(owner, repo, commit, path string) string {
	return fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		apiBase, owner, repo, (&url.URL{Path: path}).EscapedPath(), url.QueryEscape(commit))
}

// download fetches a file or directory tree at an exact commit into dest,
// overwriting existing files.
func download(owner, repo, commit, path, dest string, isDir bool) error {
	if !isDir {
		return downloadFile(owner, repo, commit, path, dest)
	}
	resp, err := apiGet(contentsURL(owner, repo, commit, path), "application/vnd.github+json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var items []struct {
		Path string `json:"path"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return fmt.Errorf("%s is not a directory at %s: %w", path, commit, err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, it := range items {
		sub := filepath.Join(dest, it.Name)
		switch it.Type {
		case "dir":
			err = download(owner, repo, commit, it.Path, sub, true)
		case "file", "symlink":
			err = downloadFile(owner, repo, commit, it.Path, sub)
		default: // ponytail: submodules are skipped, vendor them separately
			fmt.Fprintf(os.Stderr, "warning: skipping %s (type %s)\n", it.Path, it.Type)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func downloadFile(owner, repo, commit, path, dest string) error {
	return saveURL(contentsURL(owner, repo, commit, path), dest)
}

// saveURL streams a raw-content URL to dest, creating parent dirs.
func saveURL(rawURL, dest string) error {
	resp, err := apiGet(rawURL, "application/vnd.github.raw")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if dir := filepath.Dir(dest); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
