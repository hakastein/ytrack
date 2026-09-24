---
status: accepted
---

# Completion is answered by ytrack, not by cobra

[ADR-0006](0006-a-package-boundary-needs-a-second-importer.md) refused cobra's hidden
completion protocol "until there is a `completion` command". This is that command, and with it
the root gets the `--version` the specification put there. Both raise the same question
twice: cobra already has code for each, and that code touches the process — it reads the
environment, writes to the process's own stderr and to a file named by the environment, and
prints a template of its own past the renderer. ADR-0006 lets only `cmd/ytrack` touch the
process, and [ADR-0003](0003-all-output-is-one-yaml-document.md) lets only the renderer print.

The decision is four rules:

- **The protocol is answered by `ytrack`**, in `internal/cli/completion.go`, from the same tree
  that runs the commands, and `Run` intercepts it before cobra is given the call.
- **The script is not answered by `ytrack`**: cobra generates it off the live tree, and it is
  written to stdout as it stands, which is the exception
  [ADR-0003](0003-all-output-is-one-yaml-document.md) records.
- **Nothing suggested costs a request.** Entities, verbs, flags and closed sets of values are
  offered; identifiers, project codes and link phrases are not.
- **The version and the revision are read off the binary** with `debug.ReadBuildInfo`, in
  `cmd/ytrack` and handed to `Run` like the environment, not stamped into it with `-ldflags -X`.
  The version of a release is the exception: [ADR-0010](0010-a-release-carries-a-calendar-version.md).

## Why the protocol is intercepted rather than added to the tree

cobra's own handler cannot be made to keep its hands off the process. Measured against cobra
v1.10.2:

- `__complete bogus ""` writes `[Debug] [Error] unable to find a command for arguments:
  [bogus]` to the process's stderr, nothing of it reaching the writer `Run` was handed, and with
  `BASH_COMP_DEBUG_FILE` set it writes the same line into that file. The path is
  `CompErrorln` → `CompDebug` (`completions.go:245`, `957-968`), which calls `os` directly.
- `COBRA_COMPLETION_DESCRIPTIONS=false` and `YTRACK_COMPLETION_DESCRIPTIONS=false` in the
  *process's* environment take the descriptions away (`completions.go:253`, `1015-1017`),
  though `Run` was handed no such env.

Neither is a setting. Two tests hold the line: one sets both variables and asserts the
descriptions are printed anyway, the other sets `BASH_COMP_DEBUG_FILE` and asserts no file
appears. They fail the day the protocol is handed back to cobra.

A command of ytrack's own under the name `__complete` was tried and rejected.
`initCompleteCmd` adds cobra's own unconditionally and takes it back off only when the call did
*not* route to a command of that name (`completions.go:296-305`), so a namesake leaves two in
the tree; which of the two `Find` answers with is written down nowhere. In the probe ours won,
by the order they were added — not by a rule anything may rest on.

So `Run` looks at the first word of argv, and on `__complete` or `__completeNoDesc` — the names
taken from cobra's own constants — builds the tree, answers from it and never calls
`ExecuteContext`. Every generated script writes the protocol's name immediately after the name
of the binary (`bash.sh:26`, `zsh.sh:49`, `fish.sh:22`, `pwsh.ps1:49`), so the first word is
where the protocol is or the call is not a shell's. A call that reaches the protocol from
anywhere else in argv — `ytrack --limit=5 __complete issue` — still meets the root's hook and is
`bad_usage`, and no shell writes such a call.

The handler prints one suggestion to a line, its text after a tab, and a last line of a colon
and the directive, which is the form the generated script parses. It reads no environment, opens
no file and sends no request; the only thing it touches is the writer `Run` was handed. A
command line naming nothing of ytrack prints `:4` and that is all — a shell throws the tool's
stderr away (`bash.sh:48`, `zsh.sh:60`), so there is no one to refuse to.

## What is offered, and what a TAB may not cost

Offered: the entities of the root, the verbs under an entity, the flags a command takes — its
own and the inherited, `--help` among them, with pflag's backquotes taken out of the usage by
`pflag.UnquoteUsage` — and a closed set of values where a command names one in `ValidArgs`,
which today is the four shells of `completion`. A command hidden in the tree is offered to no
one, and the one hidden command there is — the stand-in cobra insists on for `help` — stands in
the tree from the start for that to mean anything here (ADR-0006).

Where the word being completed stands is read before any of it is offered. What `Find` hands
back besides the command is the rest of the line, and it is read the way cobra reads it as it
runs the call — a flag written as two words takes the word after it, one carrying an `=` takes
none — so what is left over are the arguments already written. A verb is offered where the word
being completed is the first word under its command and nowhere else; a value of a closed set
while the command's own `Args` still takes a word at that place; nothing at all where the word
is the value of a flag (_but see [ADR-0011](0011-journal-values-are-trees-and-always-a-list.md): a flag
whose values are a closed set of the binary's has them offered_), or where a flag the command keeps to itself has been written, since no
command under it takes such a flag. Anything else offers a line the same call then refuses —
`ytrack completion bash zsh` is `accepts 1 arg(s), received 2`, `ytrack project bogus list` is
`unknown command "bogus"` — and a refusal the tool talked the caller into is worse than no
suggestion.

The directive is `NoFileComp` everywhere but the one argument that *is* a path, the second of
`attachment create`. Which argument it is the command says itself, in the annotation
`ytrack.completesPathAt: "2"` — the place, counted from one — because the first argument of that
same command is a readable identifier, and a list of files where `DEV-1` is wanted is a
suggestion that is never right. At that one place the directive is `Default` and the shell
offers file names itself. The name of a flag is not that place, and neither is the value of one.

Not offered: project codes, issue and article identifiers, tag names, link phrases — everything
that only the server knows. A dynamic suggestion would send a request and spend a token on every
TAB. The dev instance answers such a question in 2–25 ms, so speed is not the objection;
invisibility is. A shell reads the tool's stdout and discards its stderr, so a `denied` — a
token that expired, an instance that is down — would arrive at the caller as "nothing matched",
which is the class of silent wrongness
[ADR-0005](0005-failure-is-a-document-and-nothing-unjudged-is-printed.md) is written against.
The metadata cache is no way around it either: it holds the custom fields of a project, not the
names of entities.

The decision is reversible in one place. The handler is one function and it has the client's
address in reach; it needs a caller willing to pay a request for a suggestion, and a way for a
refusal to be seen.

## Why the script is cobra's

A hand-written completion script would be a second copy of the surface: the entities, the verbs
and the flags would have to be listed in shell code and kept in step with the tree by hand.
cobra's generator writes a script that knows none of them — it asks the binary. A script sourced
once goes on being right as `ytrack` grows, which is the whole reason the protocol exists.

Only the `Gen*(io.Writer)` forms are called. Their `…File(filename)` counterparts are the one
thing in cobra's four generators that touches the process: they create a file.

Four shells, and what was measured of each:

- **bash** — verified live: with `bash-completion` sourced and the generated script loaded,
  `ytrack ` offers the entities and `completion`, `ytrack project ` offers `list show`,
  `ytrack project show --` offers `--fields --help`, `ytrack completion ` offers the four
  shells, and `ytrack --` offers `--help --version`. Without the `bash-completion` package the
  generated function stops at `_get_comp_words_by_ref`, which is that package's own; the help of
  `completion` says so.
- **zsh**, **fish**, **powershell** — **not verified live**: none of the three is installed on
  the machine it was built on. What is verified of them is that the script is generated,
  that it is not empty, that it names the shell and the binary, and that it carries the
  protocol's name. Their loading lines are printed by `completion --help` from the same list the
  generators are taken from, so a shell named in the help is a shell there is a script for, but
  whether each script works in its shell is untested here and is a thing for the first person
  who has that shell to say.

## Where the version comes from

`--version` is a local flag of the root, and being local is what "only on the root" means:
`project --version` is `unknown flag`. cobra's own `Version` field is left unset — it prints a
template into `OutOrStdout` past the renderer (`command.go:936-951`) and takes `-v` while `-v`
is free (`command.go:1238-1258`), and `-v` is left free. The document is three keys in order:
`version`, the name the module gives itself; `revision`, the full sha; `modified`, whether the
checkout had been edited. Each is `null` where the binary carries no stamp.

The stamp is `go build`'s own. `-ldflags -X` was rejected: it needs a package-level variable,
which a plain `go build ./cmd/ytrack` leaves empty — and empty is what a reader gets, with
nothing saying the stamp was missed. The linters do not catch it either: `gochecknoglobals`
passes `var version string` in `package main`. Meanwhile `go build` already puts the pseudo-version,
`vcs.revision` and `vcs.modified` into every binary it makes, a git tag turns `Main.Version`
into the version of that tag on its own, and a build made outside a checkout or with
`-buildvcs=false` says `revision: null`, which is the honest answer for a binary of unknown
origin.

`debug.ReadBuildInfo` is called in `cmd/ytrack`, and what it answers crosses the seam beside
`env`: the image of the binary belongs to the process, and reading it is `cmd/ytrack`'s work
(ADR-0006). It was first read in `internal/cli`, to spare `Run` a seventh parameter and keep
`--version` observable through the seam, and observable it was not. Under `go test` the module's
own test binary carries no `vcs.*` setting at all, so the only thing a test could assert was
`version: "(devel)"` with two nulls: deleting the loop over the settings, inverting the dirty bit,
or reading `vcs.time` where `vcs.revision` is read each left the whole suite green. The two lines
that turn a stamp into the document were reachable from nothing.

Handed in, the stamp is a value a probe writes. Four of them stand in `version_test.go` — a
checkout as it was committed, one that had been edited, a build made outside a checkout, and a
binary the toolchain did not build — and each of those three mutations now fails. What no probe
of `internal/cli` covers is the reading itself, the one line of `cmd/ytrack` that asks for the
stamp; the job `build` covers that, since a binary handing over nothing prints `revision: null`
where the job holds it to `$GITHUB_SHA`.

The job `build` of `.github/workflows/ci.yml` asserts the output rather than the exit code, as
every job of the workflow does: it runs `make build` with `VERSION=v0.0.0.$GITHUB_RUN_NUMBER` and
then holds `./bin/ytrack --version` to a line reading `version: "v0.0.0.$GITHUB_RUN_NUMBER"` and
one reading `revision: "$GITHUB_SHA"`. `modified` is not asserted: any unignored untracked file
in the runner's working directory makes it true without making the revision wrong.

## A build inside a linked worktree stamps another revision

`go build` run from a linked git worktree — `.claude/worktrees/<branch>` here — puts the **HEAD
of the main checkout** into `vcs.revision`, not the HEAD of the worktree, and decides
`vcs.modified` by the main checkout too. The cause is that a linked worktree's `.git` is a file
holding `gitdir: …`, while `isVCSRoot` in `cmd/go/internal/vcs/vcs.go` recognises only a `.git`
**directory** as the root of a repository, so the search walks further up. Measured: built in a
worktree at one commit, the binary printed the commit the main checkout stood at.

What this touches is any check of the form "the revision in the binary is `git rev-parse HEAD`".
In CI it holds, because a runner clones. From a linked worktree it does not, and the check has to
be made on an ordinary clone of the commit. It touches no test of `internal/cli`: a test binary
carries no `vcs.*` setting at all, and all three keys come out `null`.

## Considered and rejected

- **Leaving the protocol to cobra and setting it up.** There is nothing to set up: the debug
  writer and the two environment variables are read by `os` calls inside cobra, with no seam
  between them and the caller.
- **A `__complete` command of ytrack's own in the tree.** Leaves two commands of that name in
  the tree, and which one answers is undefined (`completions.go:296-305`).
- **A hand-written completion script.** A second copy of the surface, in shell.
- **A `--no-descriptions` flag on `completion`.** The shell already has the second name of the
  protocol, `__completeNoDesc`, and the generated script sends it; the flag would be a second
  way to say one thing.
- **`cobra.OnlyValidArgs` for the shell argument.** Its refusal — `invalid argument "tcsh" for
  "ytrack completion"` — names nothing to write instead. The command refuses with
  `unknown shell "tcsh": ytrack has a script for bash, zsh, fish, powershell`.
- **`-ldflags -X` for the version.** Empty on a plain build and invisible to the linters, and
  what it puts in the binary is a string somebody had to remember to pass, where `go build`
  stamps the revision whether anyone remembered or not.

## Consequences

A command written after this one is completed without a line of work: give it a `Short` and it
is offered with its text. A closed set of values goes in `ValidArgs`, and how many
arguments the command takes in `Args`, which is what keeps a suggestion from standing where the
command would take none. An argument that is a path is marked `ytrack.completesPathAt` with the
place it stands at, counted from one. Nothing else is offered, and the rule that nothing
suggested costs a request holds until there is a way for a refusal to reach the person pressing
TAB.

A `Short` therefore has a form to keep to, and here it is: **one line, in the present tense,
beginning with a capital, ending without a full stop, at most 60 characters, naming what the
command does** — a group says `Work with the issues of the instance`, a command names the work
itself, `Print one issue by its readable id`, and where a request goes out the line names what
the server does with it, not how the command goes about it. Sixty is what the column has room
for: cobra pads the names to the longest in the tree, a shell prints the same text beside its
suggestion, and 80 columns less the name and its padding is about what is left. Whatever does
not fit goes in `Long`, which is written on its own — a `Short` built as the first sentence of
its `Long` comes out a sentence long, which is how the three lines under `auth` came to run 72
to 91 characters and start in lower case before this rule was written. Those three and four
more that ran a little over were brought to the form with it.

A `Long` has a rule too. Its reader is an agent about to write a call, so a
sentence stays only if it changes how the call is written and nothing else tells the reader: what
the command does, in one line; what an argument is where its name does not say; the shape of the
output, as an example; values a flag takes; gotchas that fail silently. A sentence goes if the output,
the flag list or a refusal already says it, or if it explains why ytrack works as it does. Reasons
belong in an ADR. The example is built from `render` nodes and printed by `render.YAML`, the
renderer of real answers, so the help cannot show a format the command does not print.

The protocol and the script are the two outputs of `ytrack` that are not YAML documents, and
ADR-0003 says so in its own words. Both are addressed to the shell. A refusal of either is still
a document on stderr.

The binary answers "which build is this?" itself: `ytrack --version | yq '.revision'` is what
`git rev-parse HEAD` was at the build. A release needs no stamp and no variable of its own — a
git tag is enough to change what `version` says. What goes into the assets of a GitHub Release,
and how a release version is resolved, this decision does not settle;
[ADR-0010](0010-a-release-carries-a-calendar-version.md) does.
