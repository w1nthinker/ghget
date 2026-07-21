package main

import (
	"os"
	"reflect"
	"testing"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want target
		err  bool
	}{
		{
			name: "blob file",
			url:  "https://github.com/golang/go/blob/master/src/fmt/print.go",
			want: target{Owner: "golang", Repo: "go", Ref: "master", Path: "src/fmt/print.go"},
		},
		{
			name: "tree dir",
			url:  "https://github.com/golang/go/tree/go1.22.0/src/fmt",
			want: target{Owner: "golang", Repo: "go", Ref: "go1.22.0", Path: "src/fmt", IsDir: true},
		},
		{
			name: "nested path",
			url:  "https://github.com/o/r/blob/main/a/b/c.txt",
			want: target{Owner: "o", Repo: "r", Ref: "main", Path: "a/b/c.txt"},
		},
		{
			name: "gist whole",
			url:  "https://gist.github.com/octocat/aa5a315d61ae9438b18d",
			want: target{Owner: "octocat", Repo: "aa5a315d61ae9438b18d", IsDir: true, IsGist: true},
		},
		{
			name: "gist single file",
			url:  "https://gist.github.com/octocat/aa5a315d61ae9438b18d#file-hello_world-rb",
			want: target{Owner: "octocat", Repo: "aa5a315d61ae9438b18d", Path: "file-hello_world-rb", IsGist: true},
		},
		{
			name: "gist id only",
			url:  "https://gist.github.com/aa5a315d61ae9438b18d",
			want: target{Repo: "aa5a315d61ae9438b18d", IsDir: true, IsGist: true},
		},
		{name: "gist empty path", url: "https://gist.github.com/", err: true},
		{name: "repo root url", url: "https://github.com/golang/go", err: true},
		{name: "not github", url: "https://gitlab.com/o/r/blob/main/f", err: true},
		{name: "releases url", url: "https://github.com/o/r/releases/tag/v1", err: true},
		{name: "blob with no path", url: "https://github.com/o/r/blob/main", err: true},
		{name: "garbage", url: "://nope", err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURL(tt.url)
			if tt.err {
				if err == nil {
					t.Fatalf("parseURL(%q) = %+v, want error", tt.url, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseURL(%q): %v", tt.url, err)
			}
			if got != tt.want {
				t.Errorf("parseURL(%q) = %+v, want %+v", tt.url, got, tt.want)
			}
		})
	}
}

func TestMatchGistFile(t *testing.T) {
	files := map[string]string{"hello_world.rb": "u1", "My File.TXT": "u2"}
	tests := []struct {
		frag, want string
		ok         bool
	}{
		{"file-hello_world-rb", "hello_world.rb", true},
		{"hello_world.rb", "hello_world.rb", true}, // exact filename
		{"file-my-file-txt", "My File.TXT", true},
		{"file-nope-go", "", false},
	}
	for _, tt := range tests {
		got, ok := matchGistFile(files, tt.frag)
		if got != tt.want || ok != tt.ok {
			t.Errorf("matchGistFile(%q) = %q, %v; want %q, %v", tt.frag, got, ok, tt.want, tt.ok)
		}
	}
}

func TestResolve(t *testing.T) {
	t.Chdir(t.TempDir())

	// No resolver: nothing happens.
	os.WriteFile("plain.lua", []byte("x"), 0o644)
	if dest, err := resolve("plain.lua", ""); err != nil || dest != "plain.lua" {
		t.Fatalf("resolve with no resolver = %q, %v", dest, err)
	}
	if _, err := os.Stat("plain.lua"); err != nil {
		t.Fatal("file touched without a resolver")
	}

	// lua2luau: single .lua file is renamed, and the new dest returned.
	os.WriteFile("mod.lua", []byte("x"), 0o644)
	dest, err := resolve("mod.lua", "lua2luau")
	if err != nil || dest != "mod.luau" {
		t.Fatalf("resolve(mod.lua) = %q, %v; want mod.luau, nil", dest, err)
	}
	if _, err := os.Stat("mod.luau"); err != nil {
		t.Fatal("mod.luau not created:", err)
	}

	// lua2luau: .lua files inside a dir tree are renamed, others untouched.
	os.MkdirAll("vendor/sub", 0o755)
	os.WriteFile("vendor/a.lua", []byte("x"), 0o644)
	os.WriteFile("vendor/sub/b.lua", []byte("x"), 0o644)
	os.WriteFile("vendor/keep.txt", []byte("x"), 0o644)
	if dest, err = resolve("vendor", "lua2luau"); err != nil || dest != "vendor" {
		t.Fatalf("resolve(vendor) = %q, %v", dest, err)
	}
	for _, p := range []string{"vendor/a.luau", "vendor/sub/b.luau", "vendor/keep.txt"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s after resolve", p)
		}
	}

	// A custom command resolver runs as `<command> <dest>`.
	os.WriteFile("myres", []byte("#!/bin/sh\ntouch \"$1.resolved\"\n"), 0o755)
	os.WriteFile("c.lua", []byte("x"), 0o644)
	if dest, err = resolve("c.lua", "./myres"); err != nil || dest != "c.lua" {
		t.Fatalf("resolve with command = %q, %v", dest, err)
	}
	if _, err := os.Stat("c.lua.resolved"); err != nil {
		t.Error("custom resolver did not run")
	}
}

func TestStripResolver(t *testing.T) {
	args, r := stripResolver([]string{"update", "-r", "lua2luau", "a", "b"})
	if r != "lua2luau" || len(args) != 3 || args[0] != "update" || args[2] != "b" {
		t.Errorf("stripResolver = %v, %q", args, r)
	}
	args, r = stripResolver([]string{"url", "dest"})
	if r != "" || len(args) != 2 {
		t.Errorf("stripResolver no flag = %v, %q", args, r)
	}
}

func TestLockUpsertRoundtrip(t *testing.T) {
	t.Chdir(t.TempDir())

	// Missing lockfile reads as empty.
	l, err := readLock()
	if err != nil || len(l) != 0 {
		t.Fatalf("readLock on missing file = %v, %v; want empty, nil", l, err)
	}

	e1 := Entry{URL: "u1", Owner: "o", Repo: "r", Ref: "main", Path: "p",
		Dest: "vendor/p", Type: "file", Commit: "abc", FetchedAt: "2026-01-01T00:00:00Z"}
	l[e1.Dest] = e1
	e2 := Entry{URL: "u2", Owner: "o", Repo: "r", Ref: "main", Path: "d",
		Dest: "vendor/d", Type: "dir", Commit: "def", FetchedAt: "2026-01-01T00:00:00Z"}
	l[e2.Dest] = e2
	if err := writeLock(l); err != nil {
		t.Fatal(err)
	}

	// Upsert same dest: updates in place, no duplicate.
	l, err = readLock()
	if err != nil {
		t.Fatal(err)
	}
	e1.Commit = "xyz"
	l[e1.Dest] = e1
	if err := writeLock(l); err != nil {
		t.Fatal(err)
	}

	got, err := readLock()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if !reflect.DeepEqual(got[e1.Dest], e1) || !reflect.DeepEqual(got[e2.Dest], e2) {
		t.Errorf("roundtrip mismatch: %+v", got)
	}

	// Stable output: writing the same lock twice is byte-identical.
	a, _ := os.ReadFile(lockName)
	if err := writeLock(got); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(lockName)
	if string(a) != string(b) {
		t.Error("lockfile output not stable across rewrites")
	}
}
