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
