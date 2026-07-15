# ghget

Ultra-lightweight, registry-free package manager — vendor anything
straight from GitHub, pinned to a commit and lockfile-tracked. Single
static binary, no dependencies.

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

Auth (for private repos and 5,000 req/h instead of 60) is picked up
automatically, in order: `GITHUB_TOKEN`, `GH_TOKEN`, then the [gh
CLI](https://cli.github.com)'s stored login (`gh auth token`) if gh is
installed and logged in. No token found = unauthenticated, which still
works but GitHub caps it at 60 requests/hour.

## Install

### rokit

```sh
rokit add w1nthinker/ghget
```

### mise

```sh
mise use -g github:w1nthinker/ghget
```

By default mise won't install a release until it's ~24h old (its
`minimum_release_age` supply-chain cooldown), so a just-published version
fails with "no versions found matching date filter." To install a fresh
release once — without changing the setting for anything else — set the env
var for that single command only:

```sh
MISE_MINIMUM_RELEASE_AGE=0 mise use -g github:w1nthinker/ghget
```

### go install

```sh
go install github.com/w1nthinker/ghget@latest
```

Or grab a binary for your platform from
[releases](https://github.com/w1nthinker/ghget/releases).

## Roblox / Rojo / Luau

ghget is an ultra-lightweight alternative to registry-based package
managers like [Wally](https://wally.run) and [pesde](https://pesde.dev)
for pulling in Luau code. There's no registry to publish to and no release
to cut: whoever wrote the module just pushes to GitHub, and you consume it
straight from a `blob`/`tree` URL, pinned to the exact commit.

```sh
# Vendor a shared Luau module into your Rojo source tree
ghget https://github.com/someone/roblox-utils/tree/main/src/Signal src/shared/Signal

# Later, pull the latest commit on that same ref
ghget update src/shared/Signal
```

`.ghget.lock` records the exact commit per dependency, so your vendored
Luau is reproducible and diffable in code review — commit it alongside your
`rokit`/`mise` toolchain files. Good fit when the module you want was never
published to a registry, or when you'd rather not maintain registry
releases just to share code between places and games. Installs via
[rokit](https://github.com/rojo-rbx/rokit) and mise (see above), the same
tools that manage Rojo, Wally, and the rest of your Roblox toolchain.

## Releasing

Push a tag like `v0.1.0`; the release workflow cross-compiles
darwin/linux/windows binaries and attaches them to the GitHub release
with GOOS/GOARCH in the asset names, which is what rokit and mise use
to select the right one.
