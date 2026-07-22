// ghget vendors files and folders from GitHub repos, pinned to exact
// commits and tracked in .ghget.lock.
package main

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
)

const usage = `usage:
  ghget <github-url> [dest] [-r resolver]   download a blob/tree/gist URL and record it in .ghget.lock
  ghget update [-r resolver] [dest...]      re-fetch lockfile entries at their original refs
  ghget mv <dest> <new-dest>                move a vendored file/dir and re-key its lockfile entry
  ghget list                                print lockfile entries

resolvers (optional, remembered per entry, re-run on update):
  -r lua2luau    built-in: rename *.lua files to *.luau
  -r <command>   run as: <command> <dest> after each download
  -r none        on update: clear the entry's resolver`

func main() {
	args, resolver := stripResolver(os.Args[1:])
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "update":
		err = cmdUpdate(args[1:], resolver)
	case "mv":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, usage)
			os.Exit(2)
		}
		err = cmdMv(args[1], args[2])
	case "list":
		err = cmdList()
	case "-h", "--help", "help":
		fmt.Println(usage)
	default:
		dest := ""
		if len(args) > 1 {
			dest = args[1]
		}
		err = cmdGet(args[0], dest, resolver)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghget:", err)
		os.Exit(1)
	}
}

// stripResolver pulls -r/--resolver VALUE out of args, wherever it appears.
func stripResolver(args []string) ([]string, string) {
	var rest []string
	resolver := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-r" || args[i] == "--resolver" {
			if i+1 < len(args) {
				i++
				resolver = args[i]
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return rest, resolver
}

// target is a parsed .../blob|tree/... GitHub URL, or a gist URL
// (Repo holds the gist ID, Path the #file-... fragment).
type target struct {
	Owner, Repo, Ref, Path string
	IsDir, IsGist          bool
}

// parseURL parses https://github.com/OWNER/REPO/(blob|tree)/REF/PATH
// or https://gist.github.com/[OWNER/]ID[#file-...].
// ponytail: REF is assumed to be a single path segment; branch names
// containing "/" can't be disambiguated without extra API calls.
func parseURL(raw string) (target, error) {
	var t target
	u, err := url.Parse(raw)
	if err != nil {
		return t, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Host == "gist.github.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch {
		case len(parts) == 1 && parts[0] != "":
			t.Repo = parts[0]
		case len(parts) == 2:
			t.Owner, t.Repo = parts[0], parts[1]
		default:
			return t, fmt.Errorf("expected https://gist.github.com/[OWNER/]ID[#file-...], got %q", raw)
		}
		t.IsGist = true
		t.Path = u.Fragment
		t.IsDir = t.Path == ""
		return t, nil
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

func cmdGet(rawURL, dest, resolver string) error {
	t, err := parseURL(rawURL)
	if err != nil {
		return err
	}
	var commit string
	if t.IsGist {
		var files map[string]string
		commit, files, err = gistInfo(t.Repo)
		if err != nil {
			return err
		}
		if t.Path != "" {
			name, ok := matchGistFile(files, t.Path)
			if !ok {
				return fmt.Errorf("no file matching #%s in gist %s", t.Path, t.Repo)
			}
			t.Path = name // lockfile records the real filename, not the anchor
		}
		if dest == "" {
			if t.Path != "" {
				dest = t.Path
			} else {
				dest = t.Repo
			}
		}
		warnIfDirty(dest)
		err = downloadGist(files, t.Path, dest)
	} else {
		if dest == "" {
			dest = path.Base(t.Path)
		}
		commit, err = resolveCommit(t.Owner, t.Repo, t.Ref, t.Path)
		if err != nil {
			return err
		}
		warnIfDirty(dest)
		err = download(t.Owner, t.Repo, commit, t.Path, dest, t.IsDir)
	}
	if err != nil {
		return err
	}
	if resolver == "none" {
		resolver = ""
	}
	if dest, err = resolve(dest, resolver); err != nil {
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
		Dest: dest, Type: typ, Commit: commit, Gist: t.IsGist, Resolver: resolver,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeLock(lock); err != nil {
		return err
	}
	fmt.Printf("%s -> %s @ %s\n", t.Path, dest, commit[:12])
	return nil
}

// cmdUpdate re-fetches entries; a non-empty resolver overrides each
// updated entry's stored resolver ("none" clears it).
func cmdUpdate(dests []string, resolver string) error {
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
		if resolver == "none" {
			e.Resolver = ""
		} else if resolver != "" {
			e.Resolver = resolver
		}
		var commit string
		if e.Gist {
			var files map[string]string
			commit, files, err = gistInfo(e.Repo)
			if err == nil {
				warnIfDirty(d)
				err = downloadGist(files, e.Path, d)
			}
		} else {
			commit, err = resolveCommit(e.Owner, e.Repo, e.Ref, e.Path)
			if err == nil {
				warnIfDirty(d)
				err = download(e.Owner, e.Repo, commit, e.Path, d, e.Type == "dir")
			}
		}
		if err == nil {
			// A resolver may rename dest itself (e.g. lua2luau on a
			// .lua file when the resolver was just added): re-key.
			var nd string
			if nd, err = resolve(d, e.Resolver); err == nil && nd != d {
				delete(lock, d)
				d, e.Dest = nd, nd
			}
		}
		if err != nil {
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

// cmdMv moves a vendored dest on disk and re-keys its lockfile entry.
func cmdMv(old, new string) error {
	lock, err := readLock()
	if err != nil {
		return err
	}
	e, ok := lock[old]
	if !ok {
		return fmt.Errorf("no lockfile entry for %q", old)
	}
	if _, taken := lock[new]; taken {
		return fmt.Errorf("lockfile entry %q already exists", new)
	}
	if dir := filepath.Dir(new); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.Rename(old, new); err != nil {
		return err
	}
	delete(lock, old)
	e.Dest = new
	lock[new] = e
	if err := writeLock(lock); err != nil {
		return err
	}
	fmt.Printf("%s -> %s\n", old, new)
	return nil
}

func cmdList() error {
	lock, err := readLock()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "DEST\tTYPE\tSOURCE\tREF\tCOMMIT\tRESOLVER\tFETCHED")
	for _, e := range sortedEntries(lock) {
		fmt.Fprintf(w, "%s\t%s\t%s/%s/%s\t%s\t%.12s\t%s\t%s\n",
			e.Dest, e.Type, e.Owner, e.Repo, e.Path, e.Ref, e.Commit, e.Resolver, e.FetchedAt)
	}
	return w.Flush()
}

// resolve post-processes dest after a download. resolver is "" (no-op),
// "lua2luau" (built-in: rename *.lua files to *.luau), or a command run
// as `<command> <dest>`. Returns the possibly-renamed dest.
func resolve(dest, resolver string) (string, error) {
	switch resolver {
	case "", "none":
		return dest, nil
	case "lua2luau":
		return lua2luau(dest)
	}
	cmd := exec.Command(resolver, dest)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return dest, fmt.Errorf("resolver %s %s: %w", resolver, dest, err)
	}
	return dest, nil
}

func lua2luau(dest string) (string, error) {
	if strings.HasSuffix(dest, ".lua") {
		return dest + "u", os.Rename(dest, dest+"u")
	}
	if fi, err := os.Stat(dest); err != nil || !fi.IsDir() {
		return dest, nil
	}
	return dest, filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".lua") {
			return err
		}
		return os.Rename(p, p+"u")
	})
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
