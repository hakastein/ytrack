---
status: accepted
---

# A release carries a calendar version

[ADR-0009](0009-completion-is-answered-by-ytrack-not-by-cobra.md) read the version off the stamp
`go build` leaves in the binary and rejected `-ldflags -X`, on the ground that "a git tag turns
`Main.Version` into the version of that tag on its own". Delivery needs a version a person can read
off a release, and a calendar one reads without a changelog: `v<YY>.<M>.<D>.<RUN>` —
`v26.9.16.58`. For such a tag the ground does not hold.

The decision is three rules:

- **Every push to `main` is a release.** The job `release` of the GitHub Actions workflow `ci`
  runs after `go`, `ytapi` and `build`, builds `linux/amd64`, `linux/arm64`, `darwin/amd64`,
  `darwin/arm64` and `windows/amd64` with `CGO_ENABLED=0`, and `gh release create` publishes them
  and their `SHA256SUMS` as a GitHub Release of the version, which creates the tag on the commit.
- **The version is `v<YY>.<M>.<D>.<RUN>`**: the date is the commit date of the built commit in UTC,
  the number is `github.run_number` of the workflow. A re-run keeps its run number and builds the
  same commit, so a retried job gets the version the run already had; the job's clock would not.
- **A release build stamps the version with `-ldflags "-X main.version=…"`**, and `cmd/ytrack`
  writes it over `Main.Version` of the stamp before handing the stamp to `Run`. Revision and
  `modified` stay `go build`'s own.

## Why go build cannot take the version off the tag

Go reads a tag as a version of the module only when the tag's major matches the module path: a
major of 2 or more needs a `/vN` suffix. The module is `github.com/hakastein/ytrack`,
and measured with go 1.26.0 on a clean checkout, each of `v26.9.22.123`, `v26.9.22` and `v26.9.123`
at HEAD leaves `version: "v0.0.0-20260922061345-1079e2ade487"`. The first is not semver at all, the
other two are of major 26. A module path ending in `/v26` would have to move every year.

## Why the objection to -X no longer applies

ADR-0009 objected that a plain `go build` leaves the variable empty and nothing says the stamp was
missed. Here an empty variable changes nothing: the version `go build` put into the stamp stands, a
pseudo-version with the commit in it, as it did before this ADR. A release build that missed the
flag would say that pseudo-version rather than nothing, and it cannot go unnoticed either — the job
`release` holds `--version` of the built `linux/amd64` binary to `version: "$VERSION"` before
publishing, and the job `build` holds a binary built with `VERSION=v0.0.0.$GITHUB_RUN_NUMBER` to that
line, and its `revision` to the commit, on every run.

## Downloading

The files are assets of the release, so `/releases/latest/download/<file>` always leads to the
latest release, and `gh release download` without a tag takes the same.

## Considered and rejected

- **A release per merge of a release branch.** ytrack has no release branches; one release per
  merge to `main` is what makes "download the latest" mean "what is on `main`".
- **A module path with `/v26`.** Makes `go build` read the tag, at the price of an import path that
  changes every January.
- **Semver.** With a release on every push there is no one to decide what is major or minor; the
  date says how old a binary is, which is what a bug report needs.
