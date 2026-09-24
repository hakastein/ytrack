---
status: accepted
---

# A login is an address and a token taken whole

[ADR-0006](0006-a-package-boundary-needs-a-second-importer.md) gave `internal/cli` "the address and
the token" and said they are read from `env`, which is all one source needs. A login has two: the
environment and a file of login records `auth login` writes, with one instance to work
against per directory and another for everywhere. Two sources raise three questions the specification
answers only in part — which source wins, which record applies where, and where a token may be sent —
and the whole of the trouble is in the third: a resolution that is merely convenient sends a
development token to the working instance.

The decision is one resolution for every command, and three rules inside it:

- **A login is an address and a token together**, taken from one source whole. Neither source ever
  completes the other.
- **The environment is that source when it holds both variables**; a variable set to nothing counts as
  unset, and one variable set with the other unset is refused rather than half-used.
- **Otherwise the login is a record, found by the directory the call was made in**, nearest ancestor
  first, the record for everywhere last.

## The one resolution

`connect(env)` in `internal/cli/credentials.go` answers with a client, the address, and the origin the
login came from. It is called from one place, the helper `ask`, which every command that
speaks to YouTrack goes through — `send` is that helper with the answer printed, which is every command
whose document is the answer and nothing besides; a command names its call and never resolves anything
itself, so nothing has to be remembered at a call site. `auth login` is the exception by construction:
it builds its client from what was just typed, and its refusals therefore carry no origin — the token
was typed on this run, not found somewhere.

An **origin** is which of the two sources answered, and nothing inside it: `auth_from: environment` or
`auth_from: settings`. Which variable, which file, which record of it is an arrangement of ytrack's own,
and a caller reading a refusal acts on neither — they set both variables or they run `auth login`. The
token itself is never handed back out of `connect` — it lives inside the client — so no document can
print it whatever a command asks for.

The file is read only where the environment holds no login. Both variables set and the file is not
opened at all, broken or not.

## What the file is

```
~/.ytrack/            0700   the directory ytrack keeps its own things in
~/.ytrack/auth.json   0600   the login records
~/.ytrack/cache/             the metadata cache's, not touched here
```

`~/.ytrack` is a directory rather than the single file the specification names, because a metadata
cache is coming and it needs a place that is not inside the logins. `HOME` comes from `env`, as
ADR-0006 requires, and a relative `HOME` names no home directory: which file it meant would depend on
where the process was started.

`auth.json` is a JSON array of records. A record without `scope`, or with a null one, is the record for
everywhere:

```json
[
  {"url": "https://youtrack.example.com", "token": "perm-…"},
  {"scope": "/home/u/src/ytrack", "url": "http://localhost:8091", "token": "perm-…"}
]
```

JSON rather than the YAML `ytrack` prints: `encoding/json` is in the binary already, a YAML reader
would pull a library into the product for one file, and ADR-0003 keeps the format of output revisable
in one line — the bytes of a configuration file must not move when it is.

**The file is ytrack's own, and reading it is strict.** Only `auth login` and `auth logout` write it; no
caller is meant to open it, so where it is and how it is laid out is nothing a document names — `auth
status` says `auth_from: settings` and no more. No file means no records. Anything in it that is not the
records ytrack writes — not JSON, `null`, more than the one array, a member that is not an object, an
unknown key, an empty `url` or `token`, an address or a token that cannot be used, a scope that is not
absolute or is not spelled as `filepath.Clean` spells it, two records for one directory, two records
without a scope — is damage, and every command refuses it as `bad_usage` with the path under `file` and
one message that says the file is damaged. Which record and what JSON said of it stay out: a caller has
nothing to mend inside the file, and the path is all they need to hand the file over or take it away.
Using the records around damage would be a guess at what the damaged part said. `null` is on the list
because `encoding/json` puts it into a slice without a word, and a file a script truncated to `null` is
damage rather than a caller with no logins.

**The system refusing the file is not the file being broken.** A `~/.ytrack` that is a regular file, a directory
that belongs to another user after a `sudo`, a filesystem mounted read-only: the read, the write or the removal
fails for something no wording of the call and no variable would mend, so `bad_usage` — "fix the call, or the
variable the message names" — would be a lie about what to do next. Those refusals are `denied`, which ADR-0005
already reads as the address, the token or **the rights**. That is the caller's machine, as an unset `HOME` is, so
the refusal names what they have to look at: the directory ytrack keeps its things in, under `directory`, and the
system's own words in `message`, stripped of the path of the file they were said about.

**Writing keeps what it did not touch.** Each record goes back as the bytes it was read as, so the
spelling, the escapes and the order of keys of a login this call never looked at survive byte for byte;
the records are ordered on the way out — the global one first, then the scopes by their bytes — so the
same logins make the same file. The file is written beside itself inside `~/.ytrack` at 0600, synced
and renamed over, so a call that fails halfway leaves the logins as they were rather than half of them.
The directory is created at 0700 when it is not there; **the mode of a directory that already exists is
left alone** — `ytrack` owns what it writes, not what it found. Taking out the last record takes the
file away and leaves the directory, which is also the cache's.

There is no lock. Two `auth login`s at the same moment can lose one of the two records, and the loss
surfaces as a `denied` naming every place that was looked in — not as a call made against the wrong
instance.

## A record is found by directory

A **scope** is an absolute physical path, and the record for everywhere has none; `auth login` and
`auth logout` print it as `global`, which no directory is spelled as, so the word cannot be read back
as one. It applies in its own directory and in everything under it, matched by whole path components:
`/a/b` holds in `/a/b/c` and not in `/a/bc`, and `/` holds everywhere. Of the records that cover the
directory, the nearest wins — the longest scope, an exact match being an ancestor at no distance — and
the record for everywhere is used only where no scope covers the directory at all.

Ancestors are what make the rule usable: without them `cd internal/cli` inside a checkout would fall
through to the record for everywhere, which is the working instance.

**A token never leaves the address it was written down beside.** The first record of the chain gives
both values, and a record holds its own address, so there is no resolution in which a token reaches an
instance other than its own. Half an environment is what makes this a rule rather than an accident:
`YTRACK_URL=https://youtrack.example.com ytrack …` in a development checkout is refused as `bad_usage`
instead of putting the development token in a header bound for the working instance. Switching instance for one
call means giving the token of that instance in the same breath —
`YTRACK_URL=… YTRACK_TOKEN=… ytrack …` — or running the call where a record for it applies.

Refusing the half is the point. Filling it in from the records would quietly drop the variable the
caller did set, and the call would go to the very instance the prefix was meant to move it off.

## Which directory the call was made in

`PWD` from `env` is the only place `internal/cli` learns the directory from, and it is believed only
while it names the directory relative names actually resolve against: it must be absolute, and the
directory it names must be the same file as `.`. It is then resolved through symlinks, because a record
made under one name of a directory would otherwise not be found under another. The check is the one
`os.Getwd` makes, for the reason it makes it:

| Launcher, child started with the working directory `/` | `PWD` the child is handed |
|---|---|
| `python3` `subprocess.run(cwd="/")`, `node` `spawnSync({cwd:"/"})` | `/tmp` — the parent's, stale |
| `sh -c`, `bash -c`, `make -C /` | `/` — a shell fixes `PWD` itself |
| `env -i` | not set |

Believing `PWD` blindly would silently choose the record of another directory, which is another
instance — the very thing a record per directory is for.

The directory is asked for only where the answer changes something: when the file holds a record with a
scope, and for `auth login` and `auth logout` without `--global`. Where it is needed and cannot be had,
the refusal is `bad_usage`; there is no quiet fall back on the record for everywhere, since which record
applies cannot be told from another directory.

The cost is that `internal/cli` stats `.`, and so depends on the working directory of the process.
ADR-0006's list of what only `cmd/ytrack` touches does not name that directory, and this is the entry
that says it does now. The same dependency belongs to every relative path a command is given —
`attachment create <path>` — and to the environment `crypto/x509` reads for
`SSL_CERT_FILE` and `SSL_CERT_DIR`, which, like the proxy variables, are the process's rather than
`Run`'s.

## The address has one spelling

An address is usable when it is an absolute `http` or `https` URL with a host and without a query or a
fragment; it is then written one way — scheme and host in lower case, the trailing slashes of the path
off, the rest as given. That spelling is what is stored in a record, what records are compared by, what
requests are built against and what is printed.

The trailing slash is not cosmetic. `GET http://localhost:8091//api/users/me?fields=login,fullName`
answers `200 text/html`, 3 825 bytes of the login page, where the same call without the doubled slash
answers `{"login":"admin","fullName":"admin","$type":"Me"}`. An address kept with its slash would reach
a page that is not the API under a status that says everything is fine.

A query or a fragment is refused rather than dropped: requests are resolved against the address, which
would lose both without saying so. A password in the userinfo is masked wherever an address is printed
— in a document, in the request of a refusal and in the words of a refusal about the address itself.

## The terminal

`auth login` asks for the token and never takes it as an argument or from a pipe. The seam carries
`stdin` as an `*os.File` for that one reason (ADR-0006): the only question asked of it is
`term.IsTerminal`, and a pipe, a file or `/dev/null` is refused as `bad_usage` with no byte read from
it, which is what makes `echo $TOKEN | ytrack auth login` impossible rather than merely discouraged.

Opening `/dev/tty` instead was rejected twice over. In `cmd/ytrack` it puts the decision in `main`,
and in `internal` it contradicts ADR-0006 outright; either way the open fails in a process with no
controlling terminal, which is what an agent is — measured in this repository's own agent shell: `tty`
answers `not a tty` and `/dev/tty` answers `No such device or address`. A prompt that cannot be
answered through `Run` could also only be exercised against a real terminal, outside the seam.

Prompts are written to the terminal that arrived as stdin: not to stdout, which carries one document,
and not to stderr, which belongs to the stream of refusals.

The dialogue goes: terminal, then the place the answer would be kept — `HOME`, `PWD` unless `--global`, and the
records themselves, read and judged — then the address with its echo, then its usability, then the token without an
echo, then the shape of that token, then `GET /api/users/me` with it, and only after a `200` the file. The place is
settled before anything is asked so that a token typed in full is not thrown away for a `PWD`, or a file, that was
never usable, and the server is asked before anything is written so that a token YouTrack refuses is refused where it
was typed rather than in some later command far from the mistake. Reading the records early does not make writing
them safe: there is no lock, so a file broken between the two still refuses on the write, only now with nothing left
to lose but the work of typing it again. The shape of the token is judged by the same rule as a token from a
variable or a record, and by the same helper: `auth login` builds its client itself, and a control byte in a pasted
token would otherwise reach `net/http` and come back as a transport failure, which says to try again.
The default is a record for the
directory rather than for everywhere: a `login` in a checkout would otherwise overwrite the record of the
working instance, and a record made in the wrong directory is visible in `auth status` and harms nothing outside
its own subtree.

Two facts about reading a terminal, measured on `x/term` v0.45.0:

- `ReadPassword` does not tell an end of input from a line: `Ctrl-D` at the token prompt is ignored
  where the address prompt, which reads its own bytes, ends the dialogue on it. The address is read a
  byte at a time for a different reason — what was typed past the return key belongs to whatever runs
  next, not to `ytrack`.
- `ReadPassword` takes the echo off after the prompt is written and puts it back on its way out, and
  it waits for a line and for nothing else. So the token is read on a goroutine, with the terminal's
  state taken before it, and a cancelled context restores the echo and ends the command; otherwise the
  first `Ctrl-C` under the handling of signals would leave a terminal with no echo.

A cancelled context and a terminal that broke are both `upstream_failed`. ADR-0005's vocabulary has no
code for "a human stopped it", inventing one here would be inventing it for every command, and
it is settled where interrupts get decided.

## The codes

| Situation | Code | Keys after `message` |
|---|---|---|
| one of the two variables set and the other not | `bad_usage` | — |
| no login anywhere | `denied` | `looked_in` |
| an address that cannot be used, from a variable or a record | `bad_usage` | — |
| the file of saved logins holds something other than the records ytrack writes | `bad_usage` | `file` |
| the system will not let the file be read, written or taken away | `denied` | `directory` |
| `PWD` does not name the directory, where that matters | `bad_usage` | — |
| a token of the environment that cannot be sent as a header | `bad_usage` | — |
| the server answered `401` or `403` | `denied` | the keys of ADR-0005, then `auth_from` |
| `auth logout` where this directory has no login of its own | `bad_usage` | — |

`auth_from` stands where the message says nothing about the login, which is the server's refusal and
`auth status`. A refusal about a variable already names it, and one about the file is the file.

`looked_in` is the list of places, in the order they were looked at: both variables, then `settings`.
The directory the saved login was looked up for is named in `message`, and where the settings were not
read at all — `HOME` unset or relative — `settings` is left off and the message says why, so that a
caller who has a saved login does not go looking for the mistake there.

`auth logout` refuses rather than doing nothing where the directory has no login of its own, and names
the login that stays in charge by where it was saved — a parent directory, or the global one. A silent
no-op would leave the caller sure they had logged out while the login of an ancestor, or the global
one, went on sending its token.

## Considered options

- **A keyring.** Process-wide shared state, a session bus an agent does not have, and the failure mode
  is already recorded in this repository: `youtrack-cli` puts the literal `[Stored in keyring]` in a
  `.env`, so sourcing it overwrites a working token and earns a `401` with no visible cause. A file at
  0600 holds the token itself, and `auth status` says which source it came from.
- **OAuth.** A browser, a redirect target and a refresh flow, for a tool whose caller is a process on a
  server. A permanent token is what YouTrack issues for exactly this use.
- **`~/.ytrack` as a single file**, which is how the specification and `AGENTS.md` name it. The
  metadata cache would then have to live somewhere else or turn the file into a directory
  later; it becomes a directory now, before anything depends on the shape.
- **Believing `PWD`.** Simpler, and tests would be free of the process's working directory. A stale
  `PWD` then silently picks the record of another directory.
- **Taking the working directory, or a function for it, into `Run`** from `cmd/ytrack`. Sound, and it
  frees the tests, at the price of a seventh parameter in the seam. Rejected on the seam, not on the
  merits; `stdin` had a reason no parameter can replace, this one has an alternative inside `internal`.
- **The environment's address with a record's token**, the literal reading of "the environment wins".
  It is the leak the first rule exists to prevent. Binding the token to the address of its own record
  closed the leak while keeping the mixture, at the price of a resolution nobody can hold in mind and a
  refusal — "no record holds that address" — that only makes sense to a reader of this file. Taking the
  pair whole makes the leak unreachable and leaves one sentence to remember.
- **Dropping half an environment silently** and reading the records anyway. The call then goes to the
  instance the caller had just overridden, and the document says `auth_from: settings` about a run they
  believed was against another server.
- **A soft refusal on a broken file** — reading the records around the broken one, as the plan's
  checklist suggests. A guess at what the file said, printed as if it were the file.
- **A warning on a plain `http://` address** (the same checklist). ADR-0005's vocabulary has no code
  for it, and the dev instance is `http://localhost:8091`.
- **`--base-url` and `--token` flags.** A token in argv is a token in the process list and in the
  shell's history. Prefixing the environment for one call does the same job with the shell's own
  machinery, which is what the specification asks for; both flags are refused as unknown.

## Consequences

Every command that speaks to YouTrack gets its client from one helper, and a `denied` the server
answered with carries `auth_from`, so "why am I not that user" is answered by the refusal rather than by
reading the source. `auth status` answers it before there is a refusal.

`YTRACK_URL` alone no longer switches instance for a call: it is `bad_usage` until `YTRACK_TOKEN` is
given with it. A caller who used the prefix against a configured instance now passes both, and
`YTRACK_TOKEN=$(cat limited-token) ytrack …` against the recorded address is gone with it.

The working directory of the process is now something `internal/cli` reads, which ADR-0006's seam
section says in one sentence.

An agent has no controlling terminal, so `auth login` is for a human: an agent works from the
environment, or from a login a human saved. `AGENTS.md` says so where an agent will meet it.

Tests point `HOME` at a temporary directory of their own and hand records the working directory of the
test process, since that is the one directory `PWD` can honestly name.
