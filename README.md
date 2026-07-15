# ghget

Vendor files and folders from GitHub repos, pinned to exact commits and
tracked in a lockfile. Single static binary, no dependencies.

## Usage

```sh
# Download a file (dest defaults to the basename)
ghget https://github.com/golang/go/blob/master/src/fmt/print.go

# Download a folder into a specific dest
ghget https://github.com/golang/go/tree/master/src/fmt vendor/fmt

# Re-fetch everything in .ghget.lock at its original ref
ghget update

# Re-fetch only specific dests
ghget update vendor/fmt

# Show what's vendored
ghget list
```

Every download resolves the ref to the exact commit that last touched the
path and records it in `.ghget.lock` (JSON, keyed by dest, diff-friendly).
`ghget update` re-resolves each entry's ref and updates `commit` /
`fetchedAt` — commit `.ghget.lock` to track exactly what you vendored.

Existing files are overwritten (it's a re-vendor); ghget warns on stderr
first if the dest has uncommitted git changes.

Set `GITHUB_TOKEN` (or `GH_TOKEN`) for private repos and higher rate
limits; unauthenticated works but GitHub caps it at 60 requests/hour.

## Install

### rokit

```sh
rokit add w1nthinker/ghget
```

### mise

```sh
mise use -g github:w1nthinker/ghget
```

### go install

```sh
go install github.com/w1nthinker/ghget@latest
```

Or grab a binary for your platform from
[releases](https://github.com/w1nthinker/ghget/releases).

## Releasing

Push a tag like `v0.1.0`; the release workflow cross-compiles
darwin/linux/windows binaries and attaches them to the GitHub release
with GOOS/GOARCH in the asset names, which is what rokit and mise use
to select the right one.
