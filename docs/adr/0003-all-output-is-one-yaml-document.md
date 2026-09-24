---
status: accepted, partly implemented
supersedes: an earlier revision of this ADR, which put prose in tilde fences and
  enumerations in TAB-separated lines
---

# All output is one YAML document

_`project show` and `project list` print the first documents: `project show` a mapping of
the project, and `project list` the counts and one flow record per line under `projects` —
strings quoted, keys bare and in the order asked. Then `auth status`, `auth login` and `auth
logout` print the first documents that are not an answer of the server: they say where
`ytrack` is pointed and where it learned that from. Then comes the first issue, and with it the
three things that were waiting for a command to print them: prose as a literal block this tool
emits itself, time in ISO-8601, and custom fields as a mapping of name to identity — beside
links as a mapping of phrase to issues. Then `issue list`: the counts of a selection, a record
to a line with the blocks of an issue inside it, and prose written as a quoted string where a
record is one line. Then `issue-history list`, whose record is not an entity at all but one
change to one: it is the first document where a count is unknown while a truncation is
certain, and the first where the text of a description reaches the caller as a value rather
than as the issue's own field. Then the links of an issue as a document of their own — `link
list`, `link add` after the write and `link remove` under `removed` — the first counts judged
by a number the specification does not declare. Then the comment: `comment create`, `comment
update` and `comment delete` print one of them as the server kept it, under an expression of
their own, which is what the `--comments` of a show does not take. Then the first value the
server writes as a path of its own: the signed link of an attachment, printed whole, and with
it the avatar of a user and the icon of a project, wherever in the document any of them
stands. Then the work item: `time list` a record of one to a line, `time create` and `time
update` the one they wrote and the issue as it stands after them, `time delete` the pair the
removed one was addressed by. With them comes the length — the ISO period of the minutes it
holds, while `presentation` and the id the server writes beside them are printed by nothing —
and the day of a work item, which is the midnight UTC it is kept at. Then the tag, the first
entity a document has to name by a pair: `tag list` a record of it to a line, `tag create` the
tag the write made, and `tag add`, `tag remove` and `tag delete` the one tag each of them moved
rather than the state of whatever it moved on. Then the two outputs that are not documents at
all — the completion script and the answer of the completion protocol — and the section below
is the exception they are printed under. The writes and the rest of the enumerations are still
to come._

The map settled the *shape* of output — one entity is markdown with frontmatter, any
enumeration is a line per element with the identifier first, and the command picks
which. It did not settle what goes in, in what order, what an empty value
looks like, or where structure ends and prose begins.

The decision is that **every command prints one YAML document**. `show` prints a
mapping for the entity, with prose as literal block scalars; `list` prints a mapping
whose `issues` key holds one flow-style record per line, alongside the counts that
describe the selection. Between the server's answer and the renderer stands a
normalisation step that owns every decision about the data, so the renderer owns only
the bytes.

Frontmatter, tilde fences and TAB-separated lines are all retired. Why the first had
to go was measured correctly the first time; why the other two were ever chosen is
recorded below, because the mistake is the useful part.

## Why frontmatter had to go

Measured on 1125 markdown files from neighbouring projects — 144 944 lines:

| Delimiter | Occurrences |
|---|---|
| `^---$` | **968** |
| ` ``` ` | 4488 |
| `^## ` | 2423 |
| `^~~~` | **0** |

And in the tracker's own prose, which is what `ytrack` actually prints: **the descriptions
of a working instance do contain lines that are exactly `---`**.

The frontmatter delimiter is a string real prose writes about itself. Its boundary is
heuristic by construction, and three independent readers confirm the failure is silent
rather than loud: `adrg/frontmatter` carries a second `---` block into the body without
an error; Pandoc's multi-YAML extension misfires in both directions; `yaml.v3` and
`goccy` decode only the first document of a stream and return `err == nil` where
PyYAML raises `ComposerError`.

## The mistake this ADR corrects

The first revision kept prose out of YAML entirely, on the grounds that `yaml.v3`
silently abandons a literal block for a double-quoted scalar on trailing whitespace —
present in 47% of the corpus files — so descriptions would arrive as one line of `\n`
escapes.

That measurement is real and it is about **an emitter's policy**, not about YAML. We
write the renderer, so the policy is ours. Measured again over 35
adversarial texts, each in three positions — a value of the document, a value nested
under another mapping, and an item of a list — against two readers, `yaml.v3` and
PyYAML:

| Input | Printed as | Round-trip |
|---|---|---|
| trailing spaces · a line of only spaces · `---` · `...` · `~~~` · a tab at the start, in the middle and at the end · non-BMP emoji · leading spaces · one and two empty first lines · no final line break, one and two · a text of line breaks alone · NBSP with ZWSP · lines beginning `# `, `- `, `: ` | literal block | exact |
| CR · NEL · LS · PS · BOM · NUL · DEL | double-quoted | exact |

Trailing whitespace — the thing that defeats `yaml.v3` and the whole of the first
revision's case — is common in the descriptions of a working instance and survives our
own emitter intact.

The second row is the correction this revision makes to the first, and it is why "it
never falls back to a quoted style" is gone from the rules below. Raw inside a literal
block, CR, NEL, LS and PS end the parse in **both** readers — the three separators are
line breaks to a YAML 1.1 reader — a CRLF turns into a bare LF in silence, and a BOM may
not stand inside a document at all. None of that is hypothetical: **U+2028 does turn up in
the descriptions and the articles of a working instance**, and the polygon measured the
server storing NEL, LS, PS, NUL and a BOM byte for byte. Refusing such an issue would
make it unreadable, and writing the rune raw would either break the document or change
the text in silence — so a text the block cannot carry goes to the writer of
double-quoted strings, the same writer that writes every other string, which loses
nothing either. A byte that is no UTF-8 is treated as unsafe for the same reason and is
the one input neither writer carries whole — `encoding/json` has already replaced it with
U+FFFD in everything the server sends, so nothing downstream of the decoder could restore
it anyway. That is a departure from the rule the specification and the first revision of
this ADR shared — that the emitter never falls back to quoting — taken because both were
written against the first row alone.

**A carriage return is the one thing that does not survive `show` → `update` of an issue, and
YouTrack is what loses it.** Measured on all three write paths — the answer to `POST
/api/issues`, a `GET` after the creation, and the answer to `POST /api/issues/{id}`
carrying a description — over 21 texts: a CRLF is stored as a lone line feed and a lone
CR is dropped outright, so `"Первая\rВторая"` reads back `"ПерваяВторая"`. A CR
therefore never comes off the server in a description at all, the loss is on the way in, and on
the way out a CR from any source is printed double-quoted and kept. Everything else of the 21 is
stored byte for byte. This is said out loud in the long help of `issue show` rather than
left to be discovered.

**The content of an article keeps the carriage return, so the loss belongs to the description
of an issue and not to prose.** Measured on the polygon over both write paths of an article and
a `GET` after each, from 15 texts up to 131071 bytes: a lone CR, a CRLF, a NEL, U+2028, U+2029,
a BOM, a NUL, spaces at either end and a text of spaces alone all come back byte for byte, and
a text of exactly `\n` does too. The one thing the server does not keep is an empty one — `""`,
`null` and no key at all are all stored as `null` — which is why an empty `--content` is refused
before the write rather than written (ADR-0005). So a round-trip `article show` → `article
update` loses nothing at all, the help of `article show` says so, and the same text through
`issue show` → `issue update` still loses its CR: what is true of a text here is true of the
member it is stored under, never of texts in general.

**The text of a comment keeps it too, at both kinds of owner**, which leaves the description of
an issue alone with the loss. Measured on the polygon over 32 texts, each written to a comment
of an issue and of an article and on both verbs, and read back both in the answer to the write
and in a `GET`: a lone CR, a CRLF, a trailing CR, a NEL, U+2028, U+2029, a BOM, a NUL, spaces
at either end, a text of whitespace alone, U+1F600 and 300 000 bytes all come back byte for
byte, and `@admin` and `+1` are stored as typed although a workflow reads the second of them.
A round-trip `issue show` → `comment update` therefore loses nothing either, and a comment
holding a CR or a line separator is printed as a double-quoted string, the same as any other
text a block cannot carry. The one thing that differs by owner is the empty one — an issue
refuses `""` outright and an article stores it — which is why an empty text is refused before
the write at both (ADR-0005).

The general lesson, which is why this is written down rather than quietly fixed: a
measurement of a library is not a measurement of a format, and the two were conflated
because the library's result arrived first and fit the argument being built. The second
lesson is the same shape: twelve inputs were measured, the twelve held, and the rule was
written as though nothing else could arrive.

## Why enumerations are YAML too

The first revision defended TAB-separated lines with `wc -l`, `awk '{print $1}'` and
`grep`. Once the output is YAML, `yq` does all three — and one thing the line form
cannot do at all:

| Want | TAB line | YAML document |
|---|---|---|
| count the records | `wc -l` | `yq '.issues \| length'` |
| take the identifier | `awk '{print $1}'` | `yq -r '.issues[].idReadable'` |
| whole record by match | `grep` | `grep` — a record is still one line |
| **filter by field *name*** | **impossible** — there are no names | `yq '.issues[] \| select(.customFields.Priority == "Critical")'` |

The defence was of the mechanism, not of the goal.

What it costs, measured on 30 issues:

| Form | 2 fields | 6 fields |
|---|---|---|
| TAB-separated | 868 tok. | 1641 tok. |
| YAML, flow record per line | 1142 tok. (**+32%**) | 2296 tok. (**+40%**) |

The overhead grows with the field count, because the key names repeat on every row.
For `show` the same comparison is **+2.0…2.9%** (6389 against 6210 tokens on one
issue; the earlier 808-against-660 figure came from a synthetic case with a 1206-byte
description, where fixed overhead dominated and the per-line indentation did not).

What it buys is larger than what it costs, by this project's own stated economics —
ADR-0001: *a turn costs far more than a verbose error.*

- **The ban on nested collections in a record disappears.** The line form had to refuse
  `issue list --fields +links(…)`, because a collection of objects inside a field is a
  format inside a format. A document has no such problem, so what was `1 + N` calls
  becomes one.
- **`null`, `""` and `[]` stay distinct for free**, where the line form needed a
  quoting convention the first revision never actually specified — a hole it shipped
  with.
- **Round-trip is free.** The first revision noted that a machine round-trip
  `show` → `update` would need a length prefix. It needs nothing.
- **No escaping language and no path language of our own** — no `\t`/`\n` sequences, no
  `comments[0].text` fence labels.

Records are written in **flow style, one per line**: block style measured identically
(1141 against 1142 tokens at two fields) while doubling the line count, and one line
per record keeps `grep` returning whole records and keeps diffs readable.

## Counts move into the document

```yaml
total: 412
returned: 30
truncated: true
issues:
  - {idReadable: "DEV-123", summary: "Ошибка при сохранении карточки клиента", customFields: {"State": "In Progress"}, created: "2026-09-10T08:14:31Z"}
```

This costs 48 tokens. The map's rule that stdout carries only records existed
because stdout was a bare stream of lines, where anything else would be counted by
`wc -l` and matched by `grep`. A document has somewhere to put what describes the
request without corrupting what answers it, and the agent stops having to capture a
second stream to learn that its selection was truncated. Diagnostics that are not
about the data — warnings, refusals — stay on stderr.

**A total the server would not say is `null`, and `truncated` is `null` with it.** The
counter of a selection answers `-1` while it is still counting, and asked twice it may
answer `-1` twice (ADR-0005); the count was therefore asked for and not given, which is
what `null` means everywhere else in this tool — ADR-0001's three states are "no key,
not asked", "`null`, asked and empty", and a value, and a document of a list always asks
for its total. `0` would say there are no issues and an absent key would say nobody
asked, both of them claims about a number the server withheld, and `"unknown"` would
break the type, so that `yq '.total > 10'` stopped comparing numbers. `truncated`
follows because it is `total > returned`: a page that fills the limit proves neither
that more were found nor that these are all of them, so `true` would assert a truncation
and `false` a completeness, and neither was checked. What that costs is one reading
habit — `yq -e .truncated` takes `null` for false — and the long help of the command
says out loud that `null` there means unknown.

**The two keys are separate, and the history is where that shows.** `truncated` follows the
total only where a total is what proves it. Nothing counts activities and asking the server
for all of them comes back silently cut to a thousand (ADR-0005), so the request asks for
one record past the limit, and the arrival of that record proves the page was cut while
saying nothing whatever about how many there are. The document is then `truncated: true`
beside `total: null`, which reads as what it is — the page is not all of them, and how many
there are is unknown — and it is why a list carries a truncation of its own rather than a
`total > returned` computed at the last moment. A tool that had only the count would have to
choose between a number it made up and a truncation it could not report.

## What follows from it

- **Keys are verbatim from `fields=`**, `idReadable` and not `id`, so the caller can
  match what arrived against what was asked. `customFields` keeps its key and renders
  as a mapping from field name to value, because ADR-0002 already made the custom
  field a model whose `name` is its identity — and nesting makes a collision with a
  native name structurally impossible.
- **A key is printed bare, so only `ytrack`'s own names are keys.** A bare key is read
  by the reader's rules, not ours, and the renderer checks every key: ASCII letters,
  digits, `_` and `$`, no leading digit, and no word a reader resolves to a bool or a
  null — `null`, `true`, `false`, `yes`, `no`, `on`, `off`, `y` or `n`, in any letter
  case. Measured on every key of up to three characters and on every case of those
  words: `yaml.v3` resolves `null`, `true` and `false` in three spellings each, PyYAML
  adds `yes`, `no`, `on` and `off`, `yaml.v2` adds `y` and `n`, and Ruby's Psych
  ignores case. All 348 property names in the specification fit, `$type` the only one
  with a `$`, and so do a refusal's keys (ADR-0005) and the counts above. Any other
  key, or a key twice in one mapping, is an error returned before a byte is written. A
  name asked for in `--fields` is printed as a key, so the expression holds every name
  to the same rule and a name that breaks it is `bad_usage` before any request. A
  key that comes from the data, such as a custom field's name or the phrase a link goes
  by, is held to no grammar and is never printed bare: it is written by the writer of
  double-quoted strings, the one that writes the values, so a name holding a colon, a
  quote or a word a reader takes for a bool comes back as the name it is. That includes
  `"State"`, which needs no quotes — quoting only where quoting is needed would be a
  heuristic over data, the class banned two bullets down. A working instance carries
  custom field names that hold dots, dashes, brackets or underscores and names that mix
  alphabets, while none of them need be a word special to YAML — so the rule is not for the names that break the grammar, it is
  for not having to know which ones do. Two such keys alike in one mapping is
  `upstream_lied`: which of them survived would be the renderer's choice rather than
  anything the server said.
- **Key order is request order.** "Identifier first" is a property of each command's
  default expression, not of the format; `--fields +x` appends. Custom fields are
  reordered by `ytrack` itself, because the server does not preserve the order: asked
  `State,Priority,Type`, it returned `Type,Priority,State`, and across the issues of an
  instance in use the array takes many different orders. Named one by one, the fields stand in the
  order they were named. Taken whole, they stand in the project's own order — the
  `ordinal` the project gave each binding, and the number of the binding where two share
  an `ordinal`, compared as numbers so that `180-9` comes before `180-10`. That order is
  read off the issue's own fields, `projectCustomField(id,ordinal)`, rather than off the
  project's field list: a token with no role on the project is sent that list as
  `200 []`, and every field of the issue would then be missing from it. Alphabetical
  order would be an invention of ours over someone else's data, and the order the array
  arrives in is the global ordinal of the prototype, which two installations of one
  polygon need not agree on.
- **Every string scalar is quoted, always.** No heuristic about when quoting is
  needed — that would be a computation over data, the class ADR-0002 banned for
  `$type` — and it is what keeps `null`, `""` and `[]` distinguishable, which is what
  all of ADR-0001 rests on.
- **Prose is a literal block scalar**, emitted by us, never by a library's default
  style. What is prose is a property of the name, not of the text: the string properties
  `description`, `text` and `content` — 13 places in the specification — and nothing
  else, so the shape of a document is never chosen by the data in it. The emitter writes
  the **indentation indicator**, always `2`, when the first **non-empty** line begins
  with a space or a tab. A reader takes the indentation of a block from that line, so one
  beginning with a space would lose it and one beginning with a tab is refused outright
  (`yaml.v3` `scannerc.go:2383`: "found a tab character where an indentation space is
  expected"); the digit is always 2 because nesting steps by two and a reader counts the
  indentation from the parent. Measured over the 35 texts in three positions against both
  readers, the condition on the *first non-empty* line loses nothing, while the condition
  on the *first* line — what an earlier revision of this ADR wrote — loses the leading
  space of "an empty line, then a line beginning with a space", which is DEV-5 of the
  polygon. Writing the indicator always loses nothing
  either and is rejected for a different reason: it puts a digit no reader needs into
  nearly every header. The **chomping indicator** is `-` where the text ends in no line
  break, which is most descriptions of a working instance, `+` where the text is line breaks alone
  or ends in two or more, and the default, which keeps exactly one, for the rest — a text
  of exactly `\n`, which the server stores, would clip to `""`. A text the block cannot
  carry is written double-quoted instead, above, and a CR is what YouTrack loses on the
  way into a description, not what the emitter loses on the way out of anything.
- **A record holding prose is a block mapping.** Elsewhere a list prints one flow record
  per line, and a literal block cannot stand in a flow record at all; an item holding
  prose anywhere below it is therefore written as a block mapping, which is what makes
  `comments` printable. Prose standing as an item of its own is an error of the renderer
  before the first byte: prose is produced under a key and nowhere else.
- **In the record of a list, prose is a quoted string instead.** Whether a text is
  written as a literal block or as a double-quoted string is a property of the place it
  stands in, decided by the command, and the rule above is the renderer's: given a
  `Prose` node under a list item it lays the item out in block style. A list answers the
  other way — normalisation produces no `Prose` in a record at all — because the two
  claims a listing makes are that a record is one line and that a record holding a
  collection is still one record, and a block mapping would spend the first to keep the
  second. It would also double the line count of every listing and break the `grep` that
  returns a whole record, which is the thing the flow record was chosen for. Nothing of
  the text is lost either way: the writer of double-quoted strings carries every byte,
  and a reader who wants the lines as lines has `yq -r '.issues[].description'` or
  `issue show`.
- **Time is normalised to ISO-8601** on the way out and on the way in. Epoch
  milliseconds identify nothing; they are the same value in a worse encoding, and
  every agent spends a turn decoding them. ADR-0002 already made ISO-8601 this tool's
  vocabulary for time, and two vocabularies for time inside one tool is the mechanism
  that got `/api/commands` rejected.
- **A length is printed out of the minutes it holds, and no name stands under it.** A
  duration is asked for bare and `ytrack` fills `minutes` in itself, because a bare
  `duration` comes back `{"$type":"DurationValue"}` and nothing more — the minutes are
  named in the request or they do not arrive. The two renderings the server writes beside
  them are out of reach rather than merely unprinted: `--fields 'duration(minutes)'`,
  `'duration(presentation)'` and `'+duration(id)'` are all `bad_usage` before any request,
  because `presentation` is written in the language of the server and reads a day as the
  working day of the instance, and `id` is the minutes as text at a work item where it is
  an ISO period at a period field — one name carrying two encodings, neither of them a
  fact about how long something is. What is printed is the period the minutes make:
  `PT1H30M` for 90, `PT1H` for 60, `PT1M` for 1, `PT24H` for 1440, `PT35791394H7M` for
  2147483647 and `PT0M` for zero, the hours unbounded so that no day of anyone's is
  implied. A duration that arrives without whole minutes is `upstream_lied` and nothing is
  printed.
- **A work item is written against a day, and the day is printed as the midnight UTC it is
  kept at.** Measured on the polygon: a body carrying noon UTC of a calendar day comes back
  and is stored as midnight UTC of that same day, so `1788220800000` prints
  `"2026-09-01T00:00:00Z"` and nothing is shifted into the time zone of whoever reads it.
  That is the ordinary printing of an instant and not a shape of its own: the name `date`
  says nothing by itself — `VcsChange.date` and `BackupError.date` are moments — so a work
  item's day is printed by the class its name falls into
  ([ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md)) like every other
  time. On the way in the calendar day and that same printed midnight are both taken, so
  what `time list` prints goes back into `time update` as it came out, and any other time
  of day is refused before the write (ADR-0005).
- **The text of a work item is prose**, by the rule above rather than by an exception to
  it: `text` is one of the three names that make prose, and the polygon keeps a work item's
  text byte for byte — a CR, a CRLF, a NEL, U+2028, a BOM, a NUL, spaces at either end —
  as the content of an article and the text of a comment do and the description of an issue
  does not. `""` is stored as `""` here rather than as `null`, which is why an empty
  `--text` is written rather than refused, and in the record `time list` prints it is a
  double-quoted string, since a record is one line.

- **An address of the instance is printed whole.** YouTrack writes its own addresses as
  paths: `url: "/api/files/12-2?sign=…&updated=1"` on an attachment, `avatarUrl:
  "/hub/api/rest/avatar/<uuid>?s=48"` on a user, `iconUrl: "/api/entityIcons/0-3"` on a
  project. A path is of no use to a reader who does not already hold the address `ytrack`
  was pointed at, and joining the two is work every agent would otherwise repeat, so each
  is resolved against that address by RFC 3986 — `url.URL.ResolveReference`, two parsed
  URLs rather than concatenated text — and printed whole. **Which names carry one is read
  off the schema the object stands at**, never off the name alone: `url` is declared on
  sixteen schemas of the specification and on several of them it arrives absolute and
  belongs to another host altogether, `ExternalIssue.url` being `https://jira.example/X-1`,
  so a table by name would rewrite somebody else's address.
  The table is `url` and `thumbnailURL` on `IssueAttachment` and `ArticleAttachment`,
  `avatarUrl` on `User`, `iconUrl` on `Project`, each with the subtypes the catalogue of
  schemas gives it — `Me` and `VcsUnresolvedUser` for the user — and nothing else. It also
  holds `ArticleAttachment.thumbnailURL`, which the specification does not declare at all
  and the instance sends on every attachment of an article regardless: that name could not
  have been derived from the schemas, which is the other half of why the table is written
  rather than computed.
  - **The schema of an object is the `$type` the server named it by, and where the server
    named none it is the one the specification declares for the place** — the same order
    the judgment of names reads a place in
    ([ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md)). `attachment list`
    settles which API answers it before it asks, so an attachment that arrived without a
    `$type` is still an attachment, and its link is resolved and held to the form below
    rather than printed as the bare path it came as: a path under a nought exit code is
    what the caller cannot tell from an address they may follow. Where neither the server
    nor the specification names a schema — `Project.customFields` holds an object of none —
    the value is printed as it arrived: what stands there is unknown, and resolving it
    would be a guess.
  - **The query crosses word for word.** `ResolveReference` takes `RawQuery` off the
    reference, so `sign=Ab-_9&updated=1` reaches the document exactly as the server wrote
    it. A signature re-encoded is a signature that no longer opens the file.
  - **An address given under a path prefix loses the prefix**, by the same rule:
    `http://h/ctx` and `/api/files/12-2` resolve to `http://h/api/files/12-2`. No instance
    behind a prefix has been measured, and this is the answer to take on purpose rather
    than an accident of the rule — YouTrack hands the same path to a browser in an `href`,
    so a deployment under a prefix has to carry the prefix inside the links it sends, and
    a tool that added one would be repairing a server that needs no repair.
  - **`null` stays `null`, and a placeholder is an address like any other**:
    `thumbnailURL` is `null` on an attachment that has no preview, `/noPreview.svg` on some
    attachments of a working instance and a path to a file on most of them. All
    three are printed by the same rule.
  - **Anything else is `upstream_lied`** ([ADR-0005](0005-failure-is-a-document-and-nothing-unjudged-is-printed.md)):
    a value that is neither null nor an absolute path — one carrying a scheme, an
    authority, an opaque part, a relative path, or `""` — resolves somewhere other than
    this instance, and printing it under a name that means this instance is exactly the
    quiet wrongness that ADR refuses. The refusal carries `field`, the place in the syntax
    of `fields=`, and `upstream_value`, what arrived word for word.
- **Identity is chosen by field type, not by the first key present.** Measured while
  drafting this: `value(name,login,…)` — the very expression ADR-0002 praised for
  reading every type — yields `"Иванов Иван"` for `Assignee`, a string the server
  refuses on write and which identifies nobody (names repeat across the accounts of a
  large instance).
  Both keys arrive; the renderer must pick from ADR-0002's identity table. The type it
  picks by is `projectCustomField.field.fieldType.valueType`, which the same request
  already carries for every field of the issue — not the issue-side `$type` this ADR
  first named, which cannot say: `SimpleIssueCustomField` stands for `date and time`,
  `integer`, `float` and `string` at once. Asking for the type is not printing it, and
  taking the type out of the same read keeps the value and its type from being two
  snapshots.
- **`$type` is not printed** on a custom field. It exists for writes, and `ytrack`
  supplies it itself; printing it would invite the agent into a decision ADR-0002 took
  away. Multiplicity stays visible as scalar versus list.
- **Empty prints where it was named and is dropped where the block was taken
  wholesale.** `resolved: null` and `comments: []` print — dropping them restores
  exactly the "not requested" versus "empty" ambiguity ADR-0001 exists to remove. But
  `customFields` taken whole prints only the non-empty: an issue returns every field it
  holds, most of them empty on a working instance, and ADR-0002 measured the set itself
  varying from issue to issue, so a printed list of empties would look complete and would
  not be. Whole, the block is several times its non-empty part. What fields a project has is `field
  list`'s question, and it answers correctly.
- **A custom field is named in `--fields` by the name its project gave it**:
  `--fields 'customFields(State,"Статус разработки")'`. A name the grammar of an
  expression carries bare — `[A-Za-z0-9_$]+` — is written bare; anything else, which is
  every Cyrillic name and every name holding a space, a bracket, a dot or a colon, goes
  in double quotes, where `"` and `\` are written `\"` and `\\`. A name has nothing under
  it, because a custom field prints as the one value it holds, and it stands only under
  the `customFields` of the issue asked for: under a link's partner or a work item it
  would pick fields out of another project. Who judges such a name is
  [ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md) — the instance's
  catalogue of custom fields, not the catalogue of schemas — and the canonical `name` it
  resolves to is what goes out and what is printed as the key. A name that reached the
  expression through the default alone is not resolved: the default pays for no request.
- **A named custom field prints empty, and one the issue does not hold prints not at
  all.** Named is named, so an empty one is `null` where the field holds one value and
  `[]` where it holds many. But a named field the issue does not hold — one its project
  never bound, or one a condition on another field hides — gets no key: `null` would say
  the issue has the field and holds nothing in it, which is a different thing to say and
  the one a write can act on. It is not `upstream_lied` either; an element absent from an
  array is data, not a key the server owed us. Where the issue holds none of the fields
  named, `customFields` prints `{}`. A field that arrives without having been named — the
  server matches by `localizedName` too — is not printed.
- **Empty link slots are not empty fields.** The server returns nine slots always,
  eight of them with `issues: []`; the phrase depends on direction —
  `sourceToTarget` when `OUTWARD`, `targetToSource` when `INWARD`, `Relates` being
  `BOTH` with an empty `targetToSource`. They normalise to a mapping from phrase to
  issues with the empty phrases dropped: 1691 bytes of wire become two lines and 59
  characters. `parent` and `subtasks` are slots of the same kind rather than fields of
  the issue — the specification declares an `IssueLink` at exactly those three names —
  so they normalise the same way, each holding one slot rather than a list; a slot the
  server sent nothing for holds no issue either and is left out too. An issue with no
  link at all prints `links: {}`: the key was named and the mapping under it is what is
  empty. The id the server addresses a slot by, `163-1s`, never reaches the document, and
  the phrase printed is the untranslated one. In `link list` an empty slot produces no
  record.
- **A tag is told from another by a pair, so every document of one carries both halves
  of it.** The name is what a caller writes and the login of the owner is what makes it
  an identity: a tag shared by whoever owns it arrives carrying a name already in the
  list, as where two owners each have a tag named `test`. So `tag list` defaults
  to `name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))`
  and a creation prints `updateSharingSettings` and `tagSharingSettings` beside them —
  the three sets a caller who has just made a tag may want to see empty, and the three
  the flags of the creation write, `--taggable-by` among them: a document leaving one of
  them out would answer nothing about the flag that wrote it. A list is a line per tag
  and carries the one set every reader of it is shown by, while a creation prints one tag
  and every set of it. `visibleFor` stands in no default of the tool
  although it is the shorter name: it carries one group where a tag is shared with
  several and `null` where it is shared with a person alone, so it would print less
  sharing than there is and no key would say so.
- **A verb that moves a tag prints the move and not the state after it.** `tag add` and
  `tag remove` print the readable id of the issue or the article and, under `added` or
  `removed`, the one tag that went on or came off; `tag delete` prints the `name` and
  the `owner` of what is gone. What the owner carries afterwards is a second snapshot
  nobody asked for, and the issues a tag hangs on are unbounded — the server puts its
  own star on every issue its owner files — so a document of the state would be a
  listing under another name. `issue show --fields tags(name)` answers that question.
- **A link record carries both ends**, subject included even though the argument already
  named it: `link list` opens with the `idReadable` of the issue it was asked about, and
  the mapping of phrase to issues follows. **Its counts are judged by `issuesSize`** —
  `total` their sum over the slots, `returned` what was printed, `truncated` the two
  disagreeing — a number the server sends beside every slot of every issue and the
  specification declares nowhere, so an absent one is `upstream_lied` rather than a name
  nobody may ask for. `total` below what arrived is `upstream_lied` too: both come off one
  answer, so it is that answer contradicting itself rather than a collection that moved
  between two requests. **There is no `--limit`.** The collection nested under `fields=`
  arrives whole — on an instance in use `issuesSize` equalled the length of `issues` on
  every non-empty slot, slots of hundreds of issues among them — while the subresource
  `…/links/{slot}/issues` cuts to 42 without `$top` and costs one request per slot, and
  cutting on the client is the case the checklist of the plan exists to name. The counts
  are there for the day a server cuts the nested collection anyway: the document would say
  so instead of looking complete.
- **Concatenating those documents into a stream takes a `---` between them, and the caller
  writes it.** An earlier revision of this entry said `for i in …; do ytrack link list $i;
  done` concatenates into a document-per-issue stream by itself. It does not, at any shape
  of the document: every document repeats the same keys, and a reader takes the second
  `idReadable` for a duplicate key of the first. stderr heads every document but the first
  with `---` itself (ADR-0005) because one command owns the whole of that stream; stdout
  carries one document per command, and nothing inside a command can write the line between
  its document and the next command's.
- **A write of a link prints what the issue holds afterwards; a removal prints what it took
  away.** `link add` is answered by the partner with the issue itself nested inside it, so
  the document above is built out of that answer and costs no second request. What it shows
  is the state the write left behind rather than the slot the call wrote in: a second
  `subtask of` comes back with the former parent gone, and an issue that becomes a duplicate
  comes back holding the duplicates a workflow carried over to it. What the same workflow
  did to the issue the call was made on is not there — `duplicates` leaves that issue in
  `Duplicate` itself, and a write of a link is answered with links, so that is read by
  `issue show`. `link remove` is answered `200` with an empty body, so it prints the identity
  of the link that is gone, under `removed`: the same mapping of phrase to issues, under a
  key of its own, because a reader taking `removed` for `links` would read a link that is
  gone as one the issue still holds. It takes no `--fields` — by the time it answers there
  is no link left to ask anything of, and the readable ids of the two ends are the whole of
  what it can say.
- **The default expression of `issue list` is
  `idReadable,summary,customFields(State,Type),created`** — which issue it is, what it
  is about, where it stands and when it was filed, the four things a reader of a
  selection asks first. The two custom fields are **named** rather than taken with the
  block: a name costs nothing on the wire and no request either, since a name reaching
  the expression through the default alone is resolved against nothing, while the block
  whole holds every field the issue has something in. Measured over the 28 issues of the
  polygon with a member's token at `$top=50`: `idReadable,summary,created` is 3 408
  bytes, the same with `State` and `Type` by name 20 028, and the same with the block
  whole 120 698 — six times the wire for up to thirty keys a line. An earlier revision
  defaulted to `idReadable` and `summary` alone, on the grounds that `State`, `Assignee`
  or `Priority` may be absent from a project of the instance, so a
  record of a project without the field would come back missing the key and the shape of
  the output would become a function of which project was queried. That is still true and
  is no longer a reason to leave the field out: a named field the issue does not hold
  gets no key, by the rule on named fields above, DOCS-1 printing `customFields: {"State": "To
  do"}` where DEV-1 prints `State` and `Type` both, and an absent key there is data about
  that issue rather than an accident of the request.
- **Comments are not a field.** `--comments=N` governs them: all by default, `0` for
  none, the last N otherwise. A boolean would grow a second flag the first time
  someone opened a forty-comment issue. The cost is that their field set is fixed by
  the tool — a departure from ADR-0001 — and it is paid for where it is taken rather than
  by there being nothing else on a comment, which is not so: a comment also holds
  `textPreview`, `attachments`, `reactions`, `pinned` and `visibility`. What is fixed is
  the comment **inside the document of its owner**, where a set that varied would make the
  shape of a `show` a function of two expressions at once; a caller who wants more of one
  comment addresses that comment, and the commands of `comment` take a `--fields` of their
  own over the default `id,author(login),created,updated,text`. `updated` stands in that
  default and in no expression `--comments` sends, because it is part of the state a write
  leaves behind — `null` on a comment nobody has changed since — while a comment under its
  owner is read for what was written rather than for what a write just did. Naming
  `comments` in `--fields` is `bad_usage` wherever an issue stands, and
  the message says `--comments`: one thing is asked for one way. At `0` neither the key
  nor anything about comments in the request is there at all, because none asked for is
  not the same as none there. **A deleted comment is left out**: YouTrack keeps it in the
  nested list with its text taken away — `deleted: true`, `text: null` — and
  `commentsCount` agrees, matching the length less the deleted in every issue where the two
  were seen to differ. The list is put in order by `created` by
  `ytrack` itself, so an answer out of order is nothing to guard against, and `deleted`
  is asked for and never printed.
- **The default expression of `issue show` is
  `idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields,links(issues(idReadable,summary)),description`**,
  and every comment with it. Structure first, prose last. `project(shortName)` repeats
  the prefix of `idReadable`, `wikifiedDescription` pays for the description twice,
  `commentsCount`, `parent` and `subtasks` duplicate the comments and the slots of
  `links`, and `attachments` is the one block whose records are dear: a whole signed link
  runs to some 180 characters, an issue of a working instance can carry a great many
  attachments, and a `show` that took them would spend tens of kilobytes on a question `attachment list` answers when
  it is asked. Every key of it reaches a reader with no role on
  the project: measured over DEV-1, DEV-2, DEV-7, DEV-8 and DOCS-1, a member's answer to
  the whole default is byte for byte an admin's.
- _Superseded by [ADR-0011](0011-journal-values-are-trees-and-always-a-list.md): the history is
  `activity list <issue>`, `target` is gone, and `added` and `removed` are always a list of scalars and
  trees._ **A record of the history is one change, and five of its seven keys are values of
  `ytrack`'s own.** The default expression is
  `timestamp,author(login),target,category,field,added,removed` — when it happened, who did
  it, which issue it belongs to, what kind of change it was, what of the issue it changed
  and what the change put there and took away — and every one of those keys reaches a
  reader of the issue. `timestamp` is ISO-8601 and `author` is the caller's own tree; the
  other five are printed as nothing like what arrived. `target` is the readable id of the
  issue the change belongs to, whether the change was to the issue itself or to a comment,
  an attachment or a work item of it, since an entity of an issue is addressed by the pair
  of the two and its own id stands under `added`. `category` is the identifier YouTrack
  keeps it under. `field` is the name the project gave a custom field, or the untranslated
  phrase a link goes by, or `null` where the category stands for no one field of the issue.
  `added` and `removed` hold each value by the one name its category gives one, which is
  the identity rule above read off the category rather than off the field type — an
  identity the caller can write back. A name written under any of the five is `bad_usage`
  before any request: there is no object below them left to ask for.
- **A change of a text hands the caller the text, and in a record that means a quoted
  string.** A description, a summary and the text of a comment arrive under `added` and
  `removed` as bare strings rather than as the properties the prose rule is written for,
  and a record of a list prints prose quoted in any case, so the whole text stands on the
  record's one line, byte for byte, by the writer that writes every other string. What it
  costs is the wire and the window: a record of a description carries the text whole at both
  ends, and on a working instance such a text runs to kilobytes and to hundreds of them, so
  the long help says which of `--category` and `--fields` leaves them out.

## Two outputs are not documents

The completion script and the protocol's answer are
addressed to the shell, not to the caller's parser: the script is shell source, and the answer
is the line-and-directive form the generated script parses. Neither carries anything that came
from the server, and a refusal of either is still a document on stderr, with a code and a
message. Everything a command says *about data* remains one YAML document.

The reasoning is in [ADR-0009](0009-completion-is-answered-by-ytrack-not-by-cobra.md).

## Considered and rejected

- **TOON.** Saves 22–45% of tokens; an independent run over 34 models puts its
  accuracy at 77.4% against YAML's 83.0% and JSON pretty's 81.0%. It also sorts keys,
  which kills request order, escapes `\n`, and on flat data costs more than plain TSV.
- **XML tags.** Anthropic recommends them for structuring a *prompt*, and the same
  engineering note says plainly there is no one-size-fits-all answer for tool output.
  Tags cannot carry verbatim prose in any case: either it is escaped, or the parser
  fails.
- **JSON.** Absolutely reliable and the natural target of half the agent-CLI
  literature, but prose stops being prose — a description arrives as
  `"первая\n---\nвторая"` — and it is the prose the agent came to read. It costs 789
  tokens against YAML's on `show` and 1791 against 1332 on a listing.
- **A length prefix** (`git cat-file --batch`) for prose. The one strategy with no
  failure mode, because the body is never scanned. Unnecessary once prose is a block
  scalar, and it cannot be checked by eye.

## Consequences

Every decision about the *data* moves into a normalisation step, and the renderer
keeps only the bytes: `Renderer interface{ Render(io.Writer, *Node) error }` plus a
map from name to renderer. Changing the output format is one line, and a JSON renderer
over the same normalised tree re-opens none of the decisions above. The condition is
that a node carries its own `Kind`: a `Scalar` prints as a quoted value when it is a
string and as a bare literal when it is a number or a boolean, a `Prose` as a literal
block scalar in YAML and as an ordinary string in JSON. Without that
distinction the renderer does not know what it may not break.

The exception above is exactly two outputs and is not open to a third. It is not a licence for
a human-readable mode, a table or a summary line: it holds only because neither output says
anything about data, and anything that does is a document. `--version` is not covered by it —
it is a document like any other.

There is no `--format` flag. The output is YAML, so `yq -o json` produces JSON without
one, and the map already rejected choosing the form by flag. The seam exists so the
decision can be *revised* in one line — as it has been once already — not so the
choice can be handed outward.

This also draws part of the package boundary
([ADR-0006](0006-a-package-boundary-needs-a-second-importer.md)): normalisation is
hand-written, the renderer is hand-written, and a tree passes between them.

The tool now depends on the consumer having a YAML reader. That dependency is taken
once, by `show`, and `list` adds nothing to it; the tool's own `--help` names `yq`.
In exchange `ytrack` ships no reader, no escaping language and no path language of its
own — which is the whole of what the first revision of this ADR was spending its
complexity on.
