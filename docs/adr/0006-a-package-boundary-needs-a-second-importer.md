---
status: accepted, partly implemented
---

# A package boundary needs a second importer

_The skeleton came first: `cmd/ytrack`; `internal/cli`, whose only export is `Run`;
`internal/render` with `Node`, `Kind`, `Renderer` and the `YAML` renderer; `internal/diag` with
the nine codes, `Fault` and the stderr stream; and `internal/youtrack` as a package comment. Then
`internal/ytapi` was generated and the adapter file put into `internal/youtrack`. `project show`
and `project list` gave the adapter its callers: they run the read flow through all five
packages. `auth status`, `auth login` and `auth logout` took `stdin` into the seam and put the
address and the token of every command behind one resolution
([ADR-0008](0008-a-login-is-an-address-and-a-token-taken-whole.md)). `field list` and `field
show` brought the custom-field model
([ADR-0002](0002-custom-field-metadata-is-a-struct-values-stay-trees.md)) and the metadata
cache. `user show` and `user list` are the first list whose records the server selects by a text
of the caller's. `issue show` is the first command whose answer is normalised out of recognition
— prose, instants, the custom fields and the link slots of an issue — and with it came the
`Prose` node. `issue list` is the first command that hands YouTrack a search of the caller's own
and the first that has something to say beside its answer: with it came the markup of a search,
the warning and the `---` of a stream that holds more than one document. `issue create`, `issue
update` and `issue delete` are the first commands that change the instance: with them came the
write body, the border a request is judged to have left at, `write_uncertain` and exit code 2,
and the signals `main` now catches. `issue-history list` is the first command whose records are
not entities but changes to one, and the first that reads a catalogue of the instance to print a
name the answer does not carry: with it came the history and the link types, a file each. `link
list`, `link add` and `link remove` are the first commands that resolve a name against the answer
of a read rather than against a catalogue of the instance, and the first write whose own answer
is judged from both of its ends; every other command is refused as `bad_usage`. `comment
create`, `comment update` and `comment delete` are the first commands that work on something
hanging from an entity rather than on the entity itself: with them came the internal id of a
child, held to its form before it can reach a path, and the read that stands before a write into
the one kind of comment the server would take in silence. `attachment list`, `attachment create`
and `attachment delete` are the first commands that take a path of the caller's own filesystem
and the first whose request body is a stream rather than a tree: with them came
`internal/cli/localfile.go`, the one place a file is opened. `tag list`, `tag create`, `tag
delete`, `tag add` and `tag remove` are the first commands whose entity is addressed by a name of
the caller's: with them came `internal/youtrack/tag.go`, where a name becomes one tag of the
catalogue the token is shown and the names of groups become the ids the sharing of a tag is
written with. `completion` and the root's `--version` are the two commands that speak to no
server at all: the first prints the script cobra generates off this very tree, the second the
stamp `go build` left in the binary; with them came the answer to the completion protocol,
written here rather than taken from cobra
([ADR-0009](0009-completion-is-answered-by-ytrack-not-by-cobra.md)), and a `Short` on every
command. All three flows below run._

ADR-0001 through ADR-0005 named the parts of `ytrack` — the tree, the passage,
normalisation, the renderer, the vocabulary of codes — and
[ADR-0004](0004-the-generator-owns-the-operation-surface.md) drew one boundary
between them, the one between generated and hand-written. None of them said where
the rest lives. This ADR does, in the words of `CONTEXT.md`: a **tree** is what
arrived or is about to leave, **normalisation** is the step that takes every
decision about the data, and a **node** is what gets printed. Where ADR-0002 and
ADR-0003 call a normalised value a tree, a node is meant.

The decision is one command, five internal packages, and one rule for which parts
get a package: **a boundary is drawn only where an importer needs a part without the
whole, or where a second subject has its own reason to change.** Everything else is a
file.

## The layout

```
cmd/ytrack          the call, the signals it is cancelled by, and os.Exit
internal/cli        cobra, Run, argument parsing, address and token, printing
internal/ytapi      generated, not one hand-written line
internal/youtrack   everything we know about YouTrack
internal/render     Node, Kind, Renderer, YAML
internal/diag       the vocabulary of codes, Fault, the stderr stream
```

`cmd/ytrack` is not one of the five: it is the process, and the only code that
touches it.

## Why only `render` and `diag` are packages of their own

A package boundary is a promise that has to be kept. Whatever crosses it is
exported, and an export stays whether or not anything still needs it. With a single
importer the boundary buys nothing and costs exactly what should have stayed inside.

`render` and `diag` are the two parts with a real second importer:

- **`render`.** `internal/youtrack` builds nodes and `internal/cli` prints them;
  `diag` builds two more, the node of a refusal and the node of a warning. Its own reason to change is the
  bytes: ADR-0003 keeps the format revisable in one line, and that line must not
  drag YouTrack along with it.
- **`diag`.** Its codes are raised on both sides of the network — in `cli` before it
  is touched, in `youtrack` around it. Kept in `cli`, they would make `youtrack`
  import the package that imports it; kept in `youtrack`, they would make `cli`
  import everything known about YouTrack in order to say `bad_usage`, which is about
  none of it. The vocabulary also changes for a reason of its own: ADR-0005 made it
  a contract — documented, added to, never renamed.

The custom-field model, the `fields=` expression and building the node get no
package: each has one consumer, and that consumer is the rest of `youtrack`. The
transport and the adapter get none for a sharper reason. A boundary around either
would export a way to the server that does not go through the passage, and the
passage cannot be bypassed only because no such way exists. Where a vector can be
made unreachable it is not merely checked (ADR-0005); a package boundary there would
turn an unreachable path into a convention.

## Inside `internal/youtrack`: files, not packages

`internal/youtrack` is everything `ytrack` knows about YouTrack, and its structure is
expressed by files, one per role:

- the public methods, the only way into the package, a file to the entity they work on: the
  issue in one, the article in another. Each holds the calls of that entity, the reads its
  writes make before they send anything, and what only that entity has — for the article, the
  line of parents a move is held against, which is read with the parent and asked for by
  nothing else;
- an entity that hangs from either of those two is a file of its own rather than two halves in
  theirs: the comment holds its three calls, what it is by default, the read before a write
  into one an issue keeps after its author took it back, and the machinery `--comments` is
  printed by, which the issue and the article both reach for. Which kind of owner a call was
  given is read **once**, into the one value that says which schema the answer stands at, which
  word a refusal uses and which API the request goes to, so the two kinds are told apart in one
  place and nothing below it reads the id a second time. Split by owner instead, the file would
  hold the same command twice and the shape of a comment in two places. The attachment is the
  second file of that kind and reads its owner the same way, and what only it holds is the file
  on its way out: the rules a name is refused by before anything is sent, the multipart written
  into the request as the file is read, and the comparison that holds the answer to the name and
  the byte count that went out. Not a byte of the caller's own filesystem is in it — what it
  takes is a name, a reader and the fault of a file that could not be opened;
- a child that hangs from one of the two rather than from either is a file of its own on the
  same grounds: the work item holds its four calls, what a record of one and the answer to a
  write into one hold by default, the read that turns the name of a type of work into the id a
  body has to carry, and the read before a removal — which is there because the removal answers
  `200` with an empty body, so what is printed has to be read while the work item is still
  there. What the file does not hold is the body a write becomes: a duration, a day and a text
  are read off the flags and turned into a tree beside the issue's and the article's, where the
  comparison of the answer already lives. The split is the same one the article settled — the
  calls stay with the entity, what its flags may say goes to the write body — and it is why a
  fourth entity that writes costs a file and not a shape;
- the tag is one file although it is two things at once — an entity of its own, made and
  destroyed by verbs of its own, and a thing that hangs from an issue or an article the way a
  comment does. What holds the two halves together is that both address the tag the same way:
  by the name a caller writes, resolved against the catalogue the token is shown, since the
  server has no search that could do it and a name resolving to nothing needs the whole listing
  to suggest from. One resolver serves all five verbs, so a name means the same thing whichever
  one was called, and the internal id it yields reaches a path from there and from nowhere else.
  Beside it stands the second resolution nothing else in the module has, the names of groups
  turned into the ids the three sets of sharing are written with, together with what a name of a
  tag may not carry and the check of each set against the answer. Which kind of owner a tagging
  was given is read once, into the one value that says which word a refusal uses and which API
  the request goes to — the same shape the comment and the attachment are built on;
- the one adapter file, the only place `internal/ytapi` is imported;
- the passage function, and within it the passage a write goes through: which calls are
  writes is settled by the passage a command sends its call through and never by the
  method, since `issue list` asks two of its questions with a `POST` and changes nothing
  (ADR-0005). The border a request is judged to have left at and the comparison of a
  write's own answer sit there, the comparison as a parameter of the passage rather than
  as a step after it, so no write can be sent without one;
- the transport: the bearer-header editor and an `http.Transport` of its own that sends
  nothing twice. Keep-alive is off: on a reused connection that breaks before the answer
  `net/http` sends the request again by itself — measured, a command that sent two
  requests reached the server three times with keep-alive on and twice with it off. A
  redirect is not followed, because a followed one is a request nobody asked for and a
  `POST` goes on as a `GET`. Only HTTP/1.1 is spoken, named as the transport's one
  protocol rather than left to follow from a dialer of its own: the HTTP/2 transport of
  `net/http` sends a request again by itself on a GOAWAY or a refused stream. Proxy
  variables are not read: they belong
  to the process's environment. **`go-retryablehttp` is not in it**:
  ADR-0005 removed retries altogether, and this is that correction carried into the
  layout;
- the `fields=` expression and the requested tree it is read into;
- the search of the caller as the server marks it up, and the parts of it the server
  looks for in the text of the issues — a file of its own rather than one of the issue,
  because it is a question about a string in YouTrack's own language and
  `issue-history list` asks it of the same endpoint about the same string;
- the document of a list: when the count is asked for, what is made of an answer that
  does not give one, and the record to a line. A truncation is kept there beside the count
  and not derived from it, because a history knows it was cut off without knowing of what:
  the request asks for one record past the limit, and the arrival of that record is the
  third source of a truncation, beside a count above the page and a count nobody gave
  ([ADR-0003](0003-all-output-is-one-yaml-document.md));
- the history of an issue: the table of categories, which is the tool's own and not the
  specification's, and what a change of each category stands on, holds and was of. It is a
  file of its own rather than one of the issue because its record is not an issue and not
  an entity at all — it is one change to one, and every block of it is printed by the row
  of that table rather than by the shape that arrived;
- the link types of the instance, read for the untranslated phrase an end of a link goes
  by. A record of the history names that end by the translated phrase alone, and nothing
  else of an answer carries the other one, so this is the one catalogue read to print a
  name the answer does not hold. It is a file of its own, beside the links of an issue
  rather than inside them: the links of an issue are turned into phrases out of the slots
  the issue itself carries, and nothing there reads the instance;
- the judgment of names and the catalogue of schemas it judges by
  ([ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md));
- the forms of identifiers, checked before any request: one file for every command and every
  entity, because the form of the string is what picks the API a command goes to — an issue,
  an article, a project code — and a grammar of its own at a call site would be a second answer
  to that question, which is how a readable id of one kind comes to be sent to the API of
  another. The internal id a child of an entity is addressed by is made there too, and as a
  type rather than as a string: one function holds the form, every request about a child takes
  the type, and no call site puts anything else into that segment of a path. What that closes
  is not a bad message but another endpoint — an empty segment turns a write into the
  collection, which files a second comment, and `..` turns it into the owner, which a deletion
  takes away. What that one function takes from its caller is the two words its refusal sends
  them back with: what the child is called, and where they read its id off. The second is a
  parameter for the same reason the first is — a comment hangs from either kind of owner and a
  work item from an issue alone — and a refusal that named the article for a work item would
  send a caller to a list an article does not keep;
- the custom-field model, and beside it the links of an issue turned into the phrase each
  one goes by — two blocks of an answer that are printed as nothing like what arrived, and
  a file each for the same reason;
another; - the custom-field model, and beside it the links of an issue — two blocks of an answer
that are printed as nothing like what arrived, and a file each for the same reason. The file of
links holds the whole of the role rather than the normalisation alone: the slots of an issue
turned into the phrase each one goes by, the catalogue those phrases are resolved against — which
is the same read, so no file here asks the instance for its link types — the grammar the id of a
slot is held to before a write leaves, the reading of a write's answer from both of its ends, and
the three documents the commands print. It is one file because it is one piece of knowledge about
YouTrack: that a link is named by a phrase and addressed by a slot, and that the two are not the
same thing . The internal id a child of an entity is addressed by is made there too, and as a type
rather than as a string: one function holds the form, every request about a child takes the type,
and no call site puts anything else into that segment of a path. What that closes is not a bad
message but another endpoint — an empty segment turns a write into the collection, which files a
second comment, and `..` turns it into the owner, which a deletion takes away; - the custom-field
model, and beside it the links of an issue turned into the phrase each one goes by — two blocks of
an answer that are printed as nothing like what arrived, and a file each for the same reason;
- the metadata cache: the metadata of a project written to disk and read back, which
  `field show` asks before the server and `field list` neither reads nor writes
  ([ADR-0002](0002-custom-field-metadata-is-a-struct-values-stay-trees.md)). It is the
  second file of the module that creates, renames and removes files of its own — the
  login records in `internal/cli` are the first — and it stays a file of its own rather
  than sharing a helper with them, because the two want opposite things of a failure:
  a login record that cannot be written ends the command, a cache that cannot be
  written is not even mentioned;
- building the node, which is normalisation. A block it prints as nothing like what
  arrived is told by the schema the place holds — the custom fields of an issue, the link
  slots, the category of an activity — and in one answer by the **name** of the place
  instead. _Of the four places below, [ADR-0011](0011-journal-values-are-trees-and-always-a-list.md)
  leaves `field` alone to the row: `target` is not asked for, and `added` and `removed` are trees._
  `ActivityItem` declares `target`, `added` and `removed` an object of no schema
  at all and `field` a `FilterField` the server sends subtypes of that the specification
  lacks, so there is nothing at those four places for a schema to tell a block by, and what
  each of them is printed as is the row of the category's to say. That is where the
  exception is allowed and how far it goes: a name tells a block only among the members of
  the record itself. The catalogue declares all four names on other schemas too, and an
  activity reaches them through its author, so the rule that carries the name carries the
  depth with it and stops at the first object below the record;
- the write body: what the flags say turned into the tree that goes out, held against the
  metadata of the project it goes to — the names resolved, the fields the project
  requires, the condition that hides one, and the comparison of the answer against what
  was sent. It is a file of its own rather than part of the issue, because a creation and
  an update differ only in the body they become and an article or a work item will differ
  in no more than that either — which the article bore out: what its flags may say and the
  body they turn into stand there beside the issue's, while the calls stay with the entity;
- mapping a failure to a code.

## Which way imports point

- `cmd/ytrack → cli`
- `cli → youtrack, render, diag`
- `youtrack → ytapi` (from the adapter file only), `render`, `diag`
- `diag → render`
- `render` and `ytapi` import nothing internal

Read backwards, every arrow is something that cannot happen: `render` knows neither
the codes nor YouTrack, `diag` does not know YouTrack, generated code is seen by one
file, and the process is seen by `cmd/ytrack` alone. A tree never leaves `youtrack`;
what `cli` receives is a node.

The arrow into `ytapi` is asserted by `scripts/ytapi-seam.go`, one of the assertions of
`make ytapi`, across the whole module, in every directory `./...` walks: no file but
the adapter file imports `internal/ytapi`, and outside it nothing from the package is
used beyond ADR-0004's list. Uses are type-checked in every build configuration found
in the module — by default, with the build tags its files use, for each operating
system they name — in-package and external tests included, and each `//go:build ignore`
program on its own. A file that no such configuration compiles does not pass silently:
the check exits with no verdict. It judges identifiers, not which method a call reaches
or what a name stands for, so it would not see an interface declared in the adapter
file carry the operations out into the rest of the package, or a type alias carry out a
type the list does not allow: the adapter file declares neither.

## Three flows

### Reading

1. `cli` parses argv: cobra and pflag refuse an unknown command or flag, a flag given
   twice or with a value that does not parse, and a wrong number of arguments.
2. `youtrack` checks what the call names — the form of an identifier, then the range of
   a limit — and builds the requested tree from the command's default expression or from
   `--fields`, checking its syntax and that each name can be printed as a key. Where a
   rule is about *where* a name was written — `comments` is filled by `--comments`, a link
   slot holds nothing but `issues`, a custom field of the issue is named and one of
   another issue is not — the place is settled by the **static type** of the position: the
   catalogue of schemas walked down from the root schema the command names, before any
   request. That is the other half of
   [ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md), which judges an
   absent key by the `$type` the server named; here nothing has been named yet, and the
   caller's own expression is all there is to judge. A call that does not assemble is
   refused as `bad_usage`, whatever the environment holds.
3. Only a call that assembled reads the environment: `cli` reads the address from `env`,
   then the token, each first for being there and then for a form that can be sent. One
   that is not there is refused as `denied`, one that cannot be sent as `bad_usage`.
4. The passage: `fields=` is emitted by a canonical walk of the deduplicated requested
   tree; the adapter makes the call through `ytapi`; the body is decoded into a tree and
   checked by its shape, on any status, and a `200` by the schema the command named —
   one object, or a list of objects; the names are judged by walking the requested tree
   against the one that arrived, stopping wherever a parent came back `null` or `[]`,
   and an absent key by the `$type` the server named
   ([ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md)). A failure at any
   of these is a refusal.
5. Normalisation turns each object that arrived into a node, and a list into a mapping
   of its counts and its records.
6. `cli` renders the node to stdout; exit code 0.

A list goes through the passage twice when as many records arrive as the limit asked
for: the second request counts the selection, either as a pass over ids alone or, where
the server has a counter of its own, as that counter. stdout is written only after every
answer has been judged, and on any refusal it is empty.

Nothing before step 4 touches the network, and the first check that fails is the
refusal, so a call with two faults is refused for the one checked first: the form of an
identifier, the range of a limit, `--fields` read from left to right for its syntax and
its names, whether the address is there, the form of the address, whether the token is
there, the form of the token. The call is checked before the environment because its
refusal says to fix the call, which no environment makes right; wherever else the
address and the token come to be read from, they are read in step 3.

### Writing

1. `cli` parses the flags, and `youtrack` holds what came in them to what the server
   would keep of it before anything is read: the form of an identifier, a native name
   inside `--field`, a title or a description YouTrack would store as something else.
2. One request brings the metadata — the project for a creation, the issue with its
   project under it for an update, which also brings the readable id the write is
   addressed by and the class the server names each field on the issue by. It is read
   afresh every time and never off the cache (ADR-0002).
3. `youtrack` resolves the field names against it and holds the call to what the project
   says: a name that does not resolve is `unknown_name`, a value the field cannot be
   given or a field the condition hides is `bad_usage`, mandatory fields absent from a
   creation or emptied by an update are `missing_required` — each of them naming
   everything of its kind at once. The write has not left, so the exit code is 1: the
   state is what it was.
4. The body is built as a tree, with `$type` copied where the server named one and taken
   from the table where it did not, and goes through the passage of a write once.
   Nothing is repeated. **Not every body is a tree**: `attachment create` sends the file
   itself, a `multipart/form-data` written into a pipe while the transport reads the other
   end of it, so the passage carries a reader whose length nobody knows and the request
   goes out chunked — which both attachment APIs take (ADR-0004). The passage composes
   none of it and buffers none of it; the border a request is judged to have left at is
   the same one, the last flush of the body included.
5. If the request left and the answer did not arrive, the refusal is
   `write_uncertain`, and the exit code is 2.
6. The answer is judged on the way back through the passage: the fields written are
   compared by identity, and a write that reports success and changed nothing is refused
   as `upstream_lied`. Every refusal from the status onwards carries the fact that the
   server answered the write, so it exits 2.
7. Normalisation turns the answer into the node of the state after the write, the
   cascade included; `cli` prints it; exit code 0.

### Refusing

A `Fault` is the value a refusal is printed from: a code, a message and the details
that follow the message — the request that went out, what the server answered, the
fields that were judged. It is raised on both sides of the network and is not printed
where it is raised. It travels up as a Go `error` to `cli.Run`, which hands it to the
stderr stream of `diag`; the stream prints its node — `code` first, then `message`,
then the details in the order they were given — with the same renderer that prints
success. stdout stays empty.

stderr belongs to the stream. `Run` creates it before the cobra tree, and the tree is
given no stderr: nothing in it writes there, and a command that has something to say
besides its answer is given the stream itself, as `issue list` is. cobra's own error
writer is discarded, and in cobra v1.10.2 that loses nothing: the error it would print
there is silenced and reaches `Run` as the refusal, its default help and usage never
report a failure there, and the rest belongs to the completion protocol, which `Run`
answers itself before cobra is given the call.

A refusal is not the only document of the stream. A **warning** is what a command has to
say about a call it goes on with — `issue list` says what of a search the server looks
for as text — and it carries no exit code: the code is the command's own, and a warning
never changes it. It is printed where it is found, so a command that warns and then
refuses prints the warning first. Because stderr can therefore hold more than one
document, the stream heads every document but the first with a `---` on a line of its
own, written in the same call as the document it belongs to, so a stream never ends on a
separator that nothing follows.

A refusal whose node the renderer rejects is still printed as a document of string
scalars — its code, its message and the renderer's error under `render_error` — and only
a renderer that cannot print even that leaves plain text. The one failure dropped is a
failed write to stderr, the last channel it could be reported on.

The exit code is decided by the `Fault`, in `diag` rather than hard-coded in `cli`,
and it carries ADR-0005's two meanings: `1` — the state is what it was; `2` — the
request left, and whether what was asked happened is unknown. `write_uncertain` exits 2,
and so does any refusal after a `2xx` answer to a write, whatever its code — an
`upstream_lied` from comparing the answer, for one: the write was answered, and a caller
rerunning a command that exited 1 would repeat it. That a write was answered is a fact
about the request rather than about the code, which is why the `Fault` carries it beside
the code and why the exit code is the `Fault`'s to decide.

**Every function `ytrack` hands cobra returns a `Fault` or nothing, and that is a
property of a type, not a convention.** A command's `RunE`, the root's guard against
the completion protocol below and the flag-error function are all built by one
internal helper, which takes a function returning `*diag.Fault`, not `error`, and
hands cobra the form it expects, checking for a nil pointer so that a typed nil never
becomes a non-nil `error`. An error that reaches `Run` and is not a `Fault` can
therefore only have come from cobra or pflag rejecting the call, and mapping it to
`bad_usage` is not a guess at its class — the guess ADR-0005 forbids.

## When `internal/youtrack` splits

`internal/youtrack` will be by far the largest package, and that is not a defect. It
is split only when one of two things appears:

- **an importer that needs a part of it without the whole** — the condition `render`
  and `diag` met;
- **a second subject with its own reason to change** — something inside that changes
  for a reason other than what YouTrack does.

Two things are not grounds, however large they grow:

- **length**, of a file or of the package;
- **the number of files.**

Size is what a deep module looks like from outside: a small interface over a lot of
knowledge. Cutting it by size would export the knowledge in order to shrink a
directory.

## The seam

```go
func Run(ctx context.Context, argv, env []string, build *debug.BuildInfo, stdin *os.File,
	stdout, stderr io.Writer) int
```

`cli.Run(ctx, argv, env, build, stdin, stdout, stderr) int` is the only entry point and
the only export of `internal/cli`. `argv` comes without the program name, `env` in the
form of `os.Environ()` and `build` as `debug.ReadBuildInfo` answered, so `cmd/ytrack`
hands the process over without converting anything. Only `cmd/ytrack` touches the process — `os.Args`, the environment, the
standard streams, `os.Exit`, and the signals. The address,
the token, the home directory `~/.ytrack` lives in and the directory a call was made
in are read from `env`, which is how a test points `ytrack` at an `httptest` server or
a cassette and never writes to a real home directory.

**The signals are the one thing `main` does beside that call**, and they are there
because the process is the only place they can be: `signal.NotifyContext` over
`os.Interrupt` and `SIGTERM` makes the context handed to `Run` the one a signal
cancels, and `context.AfterFunc(ctx, stop)` takes the handler back off the moment it
fires, so a second signal kills `ytrack` by Go's own default — measured, exit code 130
on the second Ctrl-C. Nothing below `Run` learns that a signal happened: a cancelled
context ends a request the way a given-up call does, which on a write is
`write_uncertain` and exit code 2, so the two lines in `main` buy a whole class of
answer without a branch anywhere else. What they do carry through is the word for it:
`signal.NotifyContext` cancels with a cause naming the signal, so the refusal reads
`interrupt signal received` rather than `context canceled`. Unhandled, the signal would
kill `ytrack` with nothing said about a request already on the wire, and the shell's 130
is no code of this vocabulary.

The working directory is the one part of the process `internal/cli` reaches for
directly, and it reaches for it only to judge `env`: `PWD` is believed while it names
the same directory as `.`, since a runner that hands a child a stale `PWD` would
otherwise choose the login record of another directory, and another instance with it
([ADR-0008](0008-a-login-is-an-address-and-a-token-taken-whole.md)).

**A path a command is given is a path, not a name to be worked out.** `attachment create`
is the one argument of that kind and `internal/cli/localfile.go` the one file that opens
one; nothing anywhere writes a path a caller wrote. The path reaches the kernel exactly as
it was typed, so a relative one resolves against the working directory of the process — the
directory the caller built the path from, and the one `cat` of the same argument would read
it in. Nothing is joined to anything and `PWD` is not consulted, which is what keeps a `..`
through a symlink from resolving lexically here and physically there; whichever directory
the login record was chosen by, the file is opened the same way. What the path stands for is
settled before it is opened — `os.Stat` first, and anything but a regular file is
`bad_usage` — because opening a named pipe waits for a writer that may never come, and the
command would print nothing and send nothing until it did. `-` is a file of that name: no
command reads standard input.

Two pieces
of the environment are read past `Run` by the standard library rather than by
`ytrack`: the proxy variables, which the transport does not use, and `SSL_CERT_FILE`
and `SSL_CERT_DIR`, which `crypto/x509` reads.

One more thing crosses the seam beside the environment, and it is the one `--version` prints:
`build`, the stamp `go build` left in the binary. `debug.ReadBuildInfo` reads the image of the
running binary, which belongs to the process as much as `os.Args` does, so `cmd/ytrack` reads it
and hands over what it read, whole: the three keys of the document are made on this side, where a
probe can hand `Run` a stamp of its own and hold them to it. It was first read below the seam, and
nothing could hold it to anything there — a test binary carries no `vcs.*` setting at all
([ADR-0009](0009-completion-is-answered-by-ytrack-not-by-cobra.md)).

`stdin` is an `*os.File` rather than an `io.Reader`, and that is the whole of the
choice: the one question asked of it is whether it is a terminal, and that takes a
descriptor. A reader would have been an invitation to read data from stdin, which is
what the specification refuses. `Run` reads nothing of it and hands it to the
constructor of the cobra tree, where `auth login` — the one command a human has to
answer — asks `term.IsTerminal` of it before anything else and refuses `bad_usage`
when it is not one, having read no byte of it. A nil file answers with a descriptor
no call owns, so a caller that hands none is refused rather than crashed, and every
test but `auth login`'s own hands none. cobra's `InOrStdin()` is called nowhere:
without `SetIn` it answers with the process's own stdin, which nothing in `internal`
may touch.

**Two answers cobra has of its own are written here instead**, and both for the same reason:
cobra's touch the process. The **completion protocol** is one. `Run` looks at the first word of
argv and, on `__complete` or `__completeNoDesc`, answers from the tree and never executes it,
because cobra's own answer reads the process's environment and writes to the process's stderr
and to a file the environment names
([ADR-0009](0009-completion-is-answered-by-ytrack-not-by-cobra.md)). The hook on the root stays
for the calls `Run` does not intercept: a protocol reached from anywhere else in argv, behind a
flag included, is still refused as an unknown command, and no generated script writes such a
call. The **`help` command** is the other. cobra adds it with the first subcommand, and its
answer to an unknown topic is exit code 0 — and, when it cannot print, a write to the process's
stderr and an exit of the process. A hidden stand-in takes its place and refuses the name as any
unknown command is refused. The stand-in is **not called `help`**: the help template lists a
command of that name even when it is hidden (`command.go:1952`), so only a name of its own keeps
the phantom out of `ytrack --help`. The name it is given is kept short — `no-help` — because
cobra pads the command column of the whole listing by the longest name among the commands, the
hidden ones included: `commandsMaxNameLen` is raised in `AddCommand` with no regard for
`Hidden`, and a stand-in named at length pushes every description of the root away from its
command. A stand-in's name belongs under the longest name a caller can see. The constructor puts
it in the tree itself rather than leaving that to `ExecuteC`, which the completion protocol never
reaches: the tree the protocol answers from is then the tree that runs the commands, and what is
hidden in it is hidden on both paths.

The cobra tree is built by a constructor on every call of `Run`, and nothing is kept
at package level: no variables of state, no `init()`, none of cobra's package-level
switches. `Run` can therefore be called in parallel.

## What is not there yet

- **The map from name to renderer that ADR-0003 names is absent.** There is one
  renderer and nothing to choose between, and a map at package level would be state.
  The decision is revised by the one line in `cli` that picks the renderer.
- **Prose is produced, and what is left of this entry is the shape it forces.** `issue show`
  gave the node its `Prose` constructor and the `YAML` renderer the literal block it is
  written as, so the absence is gone; what remains is that prose is produced under a key
  and nowhere else. A list item holding prose anywhere below it is written as a block
  mapping rather than as one flow record on a line, since a literal block cannot stand in
  a flow record; prose standing as an item of its own is an error of the renderer before a
  byte is written, alongside a nil node, a node no constructor built and a document that
  is not a mapping.
- **There is no rate limiter.** A command sends at most five requests, one after the
  other: `issue list` asks the assist, then the catalogue of custom fields where the
  caller named one of their own, then the selection, then the counter where as many
  records arrived as the limit asked for, and the counter once more where it answered
  `-1` (ADR-0005). `issue-history list` sends two or three — the assist, the link types of
  the instance where the selection may print the phrase a link goes by, and the history —
  and each of the three is decided before the first goes out, so the count is a property of
  the call rather than of the data. A bounded sequence of that shape is not the series the
  limiter was promised for. Nothing here is issued in parallel and each request waits for the answer
  to the one before it, so the pace is already the server's own round trip, and `429`
  and `Retry-After` occur 0 times in the specification, leaving nothing to pace against
  but an invention. The limiter arrives with the first command that sends requests
  without waiting on each, at a pace measured then.

## Consequences

Nothing below `Run` has a test surface of its own: there are no tests in `render` or
`diag`, and the node, the passage and normalisation are observed only through argv,
env, stdout, stderr and the exit code. An exported type in those packages exists for
its second importer, not for a test.

The seam carries stdin, and carrying it is what keeps the interactive `auth login`
observable: its whole dialogue is exercised through `Run`, over a pseudo-terminal, where
a prompt that cannot be answered through `Run` could be exercised only against a real
terminal, outside the seam. The alternative was to open
`/dev/tty` — in `cmd/ytrack`, which puts the decision in `main`, or in `internal`,
which the rule above forbids outright — and either way a process with no controlling
terminal, which is what an agent runs as, fails that open on every call. Prompts are
written to the terminal that arrived as stdin: not to stdout, which carries one
document, and not to stderr, which belongs to the stream.
