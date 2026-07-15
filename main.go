// ghget vendors files and folders from GitHub repos, pinned to exact
// commits and tracked in .ghget.lock.
package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"text/tabwriter"
	"time"
)

const usage = `usage:
  ghget <github-url> [dest]   download a blob/tree URL and record it in .ghget.lock
  ghget update [dest...]      re-fetch lockfile entries at their original refs
  ghget list                  print lockfile entries`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "update":
		err = cmdUpdate(os.Args[2:])
	case "list":
		err = cmdList()
	case "-h", "--help", "help":
		fmt.Println(usage)
	default:
		dest := ""
		if len(os.Args) > 2 {
			dest = os.Args[2]
		}
		err = cmdGet(os.Args[1], dest)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghget:", err)
		os.Exit(1)
	}
}

// target is a parsed .../blob|tree/... GitHub URL.
type target struct {
	Owner, Repo, Ref, Path string
	IsDir                  bool
}

// parseURL parses https://github.com/OWNER/REPO/(blob|tree)/REF/PATH.
// ponytail: REF is assumed to be a single path segment; branch names
// containing "/" can't be disambiguated without extra API calls.
func parseURL(raw string) (target, error) {
	var t target
	u, err := url.Parse(raw)
	if err != nil {
		return t, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Host != "github.com" && u.Host != "www.github.com" {
		return t, fmt.Errorf("not a github.com URL: %q", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || (parts[2] != "blob" && parts[2] != "tree") {
		return t, fmt.Errorf("expected https://github.com/OWNER/REPO/(blob|tree)/REF/PATH, got %q", raw)
	}
	t.Owner, t.Repo, t.Ref = parts[0], parts[1], parts[3]
	t.Path = strings.Join(parts[4:], "/")
	t.IsDir = parts[2] == "tree"
	return t, nil
}

func cmdGet(rawURL, dest string) error {
	t, err := parseURL(rawURL)
	if err != nil {
		return err
	}
	if dest == "" {
		dest = path.Base(t.Path)
	}
	commit, err := resolveCommit(t.Owner, t.Repo, t.Ref, t.Path)
	if err != nil {
		return err
	}
	warnIfDirty(dest)
	if err := download(t.Owner, t.Repo, commit, t.Path, dest, t.IsDir); err != nil {
		return err
	}
	lock, err := readLock()
	if err != nil {
		return err
	}
	typ := "file"
	if t.IsDir {
		typ = "dir"
	}
	lock[dest] = Entry{
		URL: rawURL, Owner: t.Owner, Repo: t.Repo, Ref: t.Ref, Path: t.Path,
		Dest: dest, Type: typ, Commit: commit,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeLock(lock); err != nil {
		return err
	}
	fmt.Printf("%s -> %s @ %s\n", t.Path, dest, commit[:12])
	return nil
}

func cmdUpdate(dests []string) error {
	lock, err := readLock()
	if err != nil {
		return err
	}
	if len(lock) == 0 {
		return fmt.Errorf("no entries in %s", lockName)
	}
	if len(dests) == 0 {
		for d := range lock {
			dests = append(dests, d)
		}
	}
	for _, d := range dests {
		e, ok := lock[d]
		if !ok {
			return fmt.Errorf("no lockfile entry for %q", d)
		}
		commit, err := resolveCommit(e.Owner, e.Repo, e.Ref, e.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		warnIfDirty(d)
		if err := download(e.Owner, e.Repo, commit, e.Path, d, e.Type == "dir"); err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		if commit == e.Commit {
			fmt.Printf("%s unchanged @ %s\n", d, commit[:12])
		} else {
			fmt.Printf("%s %s -> %s\n", d, e.Commit[:12], commit[:12])
		}
		e.Commit = commit
		e.FetchedAt = time.Now().UTC().Format(time.RFC3339)
		lock[d] = e
	}
	return writeLock(lock)
}

func cmdList() error {
	lock, err := readLock()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "DEST\tTYPE\tSOURCE\tREF\tCOMMIT\tFETCHED")
	for _, e := range sortedEntries(lock) {
		fmt.Fprintf(w, "%s\t%s\t%s/%s/%s\t%s\t%.12s\t%s\n",
			e.Dest, e.Type, e.Owner, e.Repo, e.Path, e.Ref, e.Commit, e.FetchedAt)
	}
	return w.Flush()
}

// warnIfDirty warns on stderr if dest has uncommitted git changes.
func warnIfDirty(dest string) {
	if _, err := os.Stat(dest); err != nil {
		return
	}
	out, err := exec.Command("git", "status", "--porcelain", "--", dest).Output()
	if err == nil && len(out) > 0 {
		fmt.Fprintf(os.Stderr, "warning: %s has uncommitted changes, overwriting\n", dest)
	}
}
