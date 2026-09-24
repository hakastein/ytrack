---
status: accepted, implemented
---

# Custom-field metadata is a struct, values stay trees

_The reading half came first. `field list` prints the custom fields of a project
and `field show` the one a caller names, resolved locally against `name` and
`localizedName`; the metadata is the struct this ADR decides on, read in one request
off the project; the twenty-row table carries one column — where the values a
field allows arrive from — with one contract test per row; the cache below is on disk.
Then the first values were printed and the table got its second column, the identity of
a value: `issue show` reads it out of the same request that brings the value, and
`issue show DEV-8` pins all twenty rows against the polygon at once. Then the first issue
was written and with it came the third column, the `$type` a body carries: `issue create`,
`issue update` and `issue delete` read the metadata afresh on every write, name the
mandatory fields all at once, evaluate the condition that hides a field, and hold the
answer to the write against what went out by the identity of the second column. Then the
first value that is no field of a project at all was written — the length of a work item —
and the second column held there too: `time create` and `time update` write a duration
as the minutes an ISO period comes to, and a type of work by the id the settings of the
project give it, while `project show` prints those settings so a caller can read the names off._

[ADR-0001](0001-partial-responses-are-trees-not-generated-structs.md) decoded every
response into a tree because the caller owns `fields=` and an absent key cannot be
told from an empty one. A custom field is where that rule needs a boundary, because
one custom field carries two things that arrive under different rules: its **value**,
which comes back under whatever `fields=` the caller wrote, and its **metadata** —
type, multiplicity, bundle — which comes back under an expression `ytrack` writes
itself, in full, every time.

The decision is that the boundary follows who chose the field set. Metadata is an
ordinary Go struct: `name`, `localizedName`, `valueType`, `isMultiValue`,
`canBeEmpty`, the issue-side `$type`, and where its permitted values live. The value
stays a tree node. ADR-0001's argument does not reach the metadata, and paying its
cost there buys nothing.

The struct is compared whole — that is how the second request is confirmed against
the name that was resolved — so every member of it is a comparable value. A project
that calls a field nothing of its own sends `null` rather than leaving the name out,
and that absence has one encoding: a name and whether it was given. The cache writes
the pair back as `null` or a string, so nothing translates between two shapes of it.

## What the server actually does

Measured against a working instance (YouTrack 2026.1, project DEV), not read off the
specification:

| Probe | Result |
|---|---|
| `field.fieldType` on a project custom field | carries `valueType` **and** `isMultiValue`, both correct |
| `/api/admin/customFieldSettings/types` | the instance publishes its own catalogue of 20 field types |
| `issue?fields=customFields($type,name,projectCustomField(field(fieldType(…)),bundle(values(…))))` | issue-side `$type`, type, multiplicity, bundle and every bundle value arrive **from the issue itself, in one request** — `/api/admin` is not needed |
| `issue?fields=project(customFields(…))` | the same for the project, before an issue exists |
| custom fields returned on an issue | **varies**: an issue carries a subset of the project's fields, and not the same subset from issue to issue |
| writing a field absent from the issue | reaches value resolution — an absent field is still writable |
| `value(name)` over a mixed list | the projection applies only where the value is an object; a scalar passes through whole, so one expression reads every type |
| field name lowercased, by `id`, or localized | `500 server_error` in all three cases |
| custom field without `$type` | `400 $type is required` |
| `Single…` on a multi-value field | `400 Incompatible field type: 170-24` — the field is not named |
| `Multi…` on a single-value field | `400` with a legible message and the entity id |
| enum value name that does not exist | `400` with a broken template: the type slot holds the *value*, the name slot is an unsubstituted `{1}` |
| bundle element `id` that does not exist | `400`, correct message — diagnosis is better on the key that reads worse |
| user by `login` / by `name` | `login` resolves; `name` is refused outright |
| period `{"minutes":420}` / `{"presentation":"7ч"}` / `{"id":"PT7H"}` | writes / writes / **`200`, and the field is emptied** — measured again when writing was built: an ISO `id` alone takes the value away from a field that held one and leaves an empty field empty, and `{"minutes":45,"id":"PT7H"}` stores 45, so the `id` is read nowhere and overwrites when it is alone |
| duplicate full names among users | common on a working instance, some of them threefold; duplicate logins: none |
| one field name across projects | may name a different type in each: `date` in one and `date and time` in another, `integer` in one and `enum[1]` in another |

Measured again while the reading half was built, against the dev polygon and a working
instance:

| Probe | Result |
|---|---|
| `FieldType` in the specification | declares `$type` and `id` and nothing else; `valueType` and `isMultiValue` arrive from every instance we have, so a typo inside `fieldType(…)` is judged against two names only |
| `UserBundle.values` in the specification | absent, and the server sends it anyway: the users **and groups** the bundle was built from, while the users the field allows are `aggregatedUsers` — on a working instance many times more users than `values` holds, banned ones among them |
| a user field whose `aggregatedUsers` is empty | takes anybody, a banned account included — an empty source is no restriction rather than no one |
| a path to a project by its code | exists under `/api/admin/projects` and nowhere else; outside `/admin` the specification offers a project only through an issue |
| the whole metadata of a project in one request | 6 502 B for the 28 fields of the polygon, growing with the fields; one enum field with every value of a large bundle can outweigh all the rest |
| `name` and `localizedName` within one project, letter case aside | no collision at all across the projects of a working instance — a name resolves to one field or to none |
| `ordinal` on the custom fields of a project | the place the project gave the field, and a project nobody ever ordered leaves every one of them at 0 — all ten fields of the polygon's DEMO, against 1…28 without a repeat on DEV |
| `localizedName` across instances | the polygon's `Состояние` and `Тип` go by other localized names on another instance — a translation is an instance's, not a fact about the type |

And again while the writing half was built, against the polygon, one probe per thing a
write has to know before it sends anything:

| Probe | Result |
|---|---|
| `$type` on an element of the body | mandatory: a body without it is `400 $type is required`, and the specification marks `Issue.customFields` and an issue field's `name`, `value` and `$type` all `readOnly` |
| a `$type` of the wrong class | only the multiplicity is checked: `State` sent as `SingleEnumIssueCustomField`, a `date` as `SimpleIssueCustomField` and a `date and time` as `DateIssueCustomField` all answer `200`, while a `Single…` on a multi-valued field is `400 Incompatible field type: 181-3` |
| the class the server names against the class the table sends | equal on all **twenty** rows, on one issue carrying a value of every type: the creation sends the table's class and the update of that same issue sends back the class the server named on it |
| a value named in another letter case | resolved: enum `task` → `Task`, state `ревью` → `Ревью`, group `development team` → `DEVELOPMENT Team`, login `ADMIN` → `admin` |
| a value with a trailing space, and one holding a Cyrillic `с` where the bundle holds a Latin `c` | `400 Сущность типа <the value> с указанным именем ({1}) не найдена` — the server resolves the name and nothing of ours does |
| a required field the project fills unasked | filled by the server: DEMO files an issue given no field at all with `Priority: Normal`, `Type: Bug`, `State: To do`, and DEV fills `Priority` with `Low` — while an explicit `Type: null` is `400 Поле Тип обязательно` |
| two required fields left out at once | the server names one of them, in one round trip per field |
| a field a condition hides, on an update | `400 invalid_properties` naming the condition, even where the same body sets the watched field to a value that keeps it hidden; a body that sets the watched field to a value that shows it writes both, in either order |
| the same field on a creation | `200`, and the issue is filed **without** the value — the only silent one of the three |
| a user a field's `aggregatedUsers` does not hold | `400 {"error":"","error_description":"Недопустимое значение"}` on `Assignee`, whose bundle holds `[admin]` — while the same accounts, a banned one included, are written into `Соисполнители`, whose bundle is empty: an empty source restricts nothing, measured on the way in as well as on the way out |
| the read an update needs | one request: `idReadable`, the class of every field the issue carries and the whole of the project's metadata under `project(…)`, about 15 KB on an issue of the polygon carrying 27 of the project's 28 fields |

And again for the first value this tool writes that is no field of a project at all — the
length of a work item, which is a `DurationValue` where a period field holds a
`PeriodValue` — measured against the polygon, one probe per thing the writing of one has to
settle:

| Probe | Result |
|---|---|
| `{"duration":{"id":"PT2H"}}` alone, on a creation and on an update | `400` both times, `Для единицы работы должна быть задана длительность`, `error_developer_message: Work item duration should be set` — the same body a period field takes with a `200` while emptying itself |
| `{"minutes":45,"id":"PT7H"}` | 45 minutes stored: the `id` is read nowhere here either |
| `{"minutes":30,"presentation":"3ч"}` | `400 Конфликт в значении периода` — the two renderings are held against each other |
| `{"presentation":"1д"}` alone | `200` and **480 minutes**: a day of YouTrack is the working day of the instance, and both instances we have keep a working week of five days of 480 minutes |
| `{"minutes":0}`, a negative one, and `2^31` | `400 invalid_properties`, `Длительность работы не может быть отрицательной или пустой` |
| `{"minutes":1.5}` | stored as `1`, in silence |
| `{"minutes":2147483647}` | `200`. The sum the issue carries is an int32 as well: on an issue holding nothing else `Затраченное время` reads 2147483647 minutes, and the first work item written after it — one minute or a million — makes the field `null` outright rather than a sum of its own. The work item itself keeps its own length either way |
| a bare `duration` in `fields=` | `{"$type":"DurationValue"}` and nothing else — the minutes are asked for by name or they do not arrive |
| `{"type":{"name":"Разработка"}}` | `400 …укажите ее ID` — a type of work goes out by id and by nothing else |
| a type of the global catalogue the project does not write against | `400 invalid_properties`, `Выбранный тип работы не поддерживается в этом проекте YouTrack`, on a creation and on an update alike |
| `{"type":null}` and `{"text":null}` on an update | each empties its own part, and a body naming one part leaves every other part of the work item as it stood |
| `{"duration":null}` and `{"date":null}` on an update | `400 Field duration cannot be null`, `400 Field date cannot be null` — neither part can be emptied at all |
| a body carrying noon UTC of a day under `date` | midnight UTC of that same calendar day is what comes back and what is stored |

## Why the metadata is not a tree

The tree exists to preserve a shape the caller chose. Nothing about field metadata is
chosen by the caller: `ytrack` asks for the same seven properties every time and gets
all of them. There is no "not requested" state to represent, so a tree here trades
compile-time safety for nothing.

The value is the opposite case. Its shape is a function of `fields=` — the caller may
ask for `value(name)`, `value(login)` or bare `value` — and the same key is an object,
an array, a scalar or `null` depending on `$type`. That is exactly ADR-0001's subject.

## `$type` is copied, not computed

`$type` is mandatory in write bodies. `ytrack` never derives it from a name:

- where the server named it — the field is present on the issue, and its
  `customFields($type,name)` already says `MultiEnumIssueCustomField` — that string is
  used verbatim;
- where the server did not — the field is absent from the issue and still writable —
  it comes from a 20-row table keyed by `(valueType, isMultiValue)`.

The second branch cannot be dropped: the field set on an issue is conditional, and
`Причина отклонения` stands on an issue only while `State = Отклонена`. The table is irregular and
that is the whole reason it is written out rather than derived: `state` has no
multi-valued form, `integer` / `float` / `string` / `date and time` all collapse into
`SimpleIssueCustomField`, and `date` gets a class of its own.

**A `200` proves less about the column than it looks.** The server reads an element of
the body for its multiplicity and for nothing else: a `State` sent as
`SingleEnumIssueCustomField` is taken, a `date` sent as `SimpleIssueCustomField` is
taken, and only a `Single…` on a multi-valued field is refused — `400 Incompatible
field type: 181-3`. So the instance accepting a body pins the multiplicity of a row and
leaves its exact string unwitnessed, and what pins the string is the **agreement of the
two branches**: one issue is filed with a value of every one of the twenty types, where
every class comes from the table, and then updated over the same twenty fields, where
every class is copied off the answer the server gave about that issue. The bodies of the
two requests stand in the recorded log side by side, and the class under each `name`
is compared row by row — twenty of twenty agreed. Neither branch is compared against a
literal of the table: that would only assert the table against itself.

The table has one row per type and a column per question asked of the type, and a
column arrives with the first caller that asks it. Reading asks where the values of a
field live, and the second the identity a value is printed and written by. `$type` is
asked by a write body and by nothing else: no read sends one, nothing prints one —
ADR-0003 keeps the server's own names out of the output — and the agreement of the two
branches can only be seen where both are sent. The column, its test per row and the test
of the agreement therefore arrived with the first command that writes, and the
three columns are what the table carries today.

This is what makes `youtrack-cli`'s defect unrepeatable rather than merely fixed. It
read multiplicity out of the project field's `$type`, where YouTrack does not put it.
The mistake was not a missing branch; it was looking at the wrong place while a right
one — `fieldType.isMultiValue` — sat beside it.

## Identity is the API's key, not the interface's label

Each value type has one key that identifies it, and it is never the string the web UI
shows:

| valueType | identity | accepted on write |
|---|---|---|
| enum, state, version, build, ownedField | `name` | `name` or `id` |
| user | `login` | `login` or `id`; `name` is refused |
| group | `name` | `name` |
| period | ISO-8601 duration | `minutes` (`presentation` parses but is localized; an ISO `id` alone empties the field) |
| date | ISO-8601 date | the scalar |
| date and time | ISO-8601 date and time | the scalar |
| integer, float, string | the scalar itself | the scalar |
| text | the text itself | the scalar |

The two rows this table used to leave out are `group`, whose identity is the group's
`name` — written as `{name}` and answered with the same `name`, for a team of a project
as for a nested group — and `text`, which is identified by its text like the other
scalars. Where a date used to be "the scalar itself", it is ISO-8601 on the way in and
on the way out: [ADR-0003](0003-all-output-is-one-yaml-document.md) settles the shape of
a time, and it is the stronger of the two here.

A user's full name identifies nobody — names repeat across the accounts of a working
instance — and YouTrack will not resolve one.

**A period is printed out of `minutes`, not out of the ISO duration the server writes
beside them.** The working week of the instance is in both of the server's own
renderings: `presentation` says `1д 3ч` for 660 minutes, so a day is eight hours there
and something else elsewhere, and the `id` says `P1D` for 480 minutes and `P3DT3H15M`
for 1635 — measured on the period values of a working instance, with `PT0S`
at zero. Any ISO-8601 reader takes `P1D` for 24 hours, so passing the server's `id`
through would be handing on a duration that means one thing here and another anywhere
else. What `ytrack` prints is built from the minutes — `PT{H}H{M}M` with the hours
unbounded, `PT0M` at zero — which means the same on every instance and is exactly what a
write turns back into minutes. `ytrack` accepts ISO-8601 on input and converts to
`minutes` itself, because ISO is what the server returns on read and silently discards
on write.

**The length of a work item is that same identity, and the work item is where the server
agrees with it.** A `DurationValue` arrives as `{id: "90", minutes: 90, presentation: "1ч
30м"}` — the `id` here is the minutes written as text rather than an ISO period, and
`presentation` is localized and reads a day as eight hours — so the ISO period built from
`minutes` is again the one thing that means the same on every instance. It is the identity in
both directions: `ytrack` prints `PT1H30M` and writes `{"duration":{"minutes":90}}`, which is
one length said twice. Where a period field takes `{"id":"PT7H"}` under a `200` and empties
itself, a work item answers `400 Для единицы работы должна быть задана длительность` — the
same input, honestly refused by one entity and silently destructive at the other, which is
why what a value is written by is settled by this table rather than by whichever key the
server happens to read.

**Days and weeks are refused rather than converted.** The body `{"presentation":"1д"}` is
taken and stored as 480 minutes, so `P1D` of YouTrack is the working day of the instance, and
turning a caller's `P1D` into minutes would be agreeing with the arithmetic of one installation
— five days of 480 minutes on both of ours and something else elsewhere. So the grammar of the input is
`PT…H…M` and nothing more: a day, a week, a second and a fraction are refused before the
request, which also puts the silent `1.5 → 1` above out of reach. Zero passes the grammar and
is left to the server, which refuses it in its own words; only a length past `2^31` is refused
locally, since the server would overflow it and answer that it is negative.

**A date is printed as the date it is in UTC**, because that is the date that was set:
every date value on that same instance is stored at 12:00 UTC, whatever time of
day was written, so noon UTC is the server's own way of saying "a day, not a moment" and
no time zone shifts it off that day. A `date and time` keeps its moment and is printed in
UTC like every other instant (ADR-0003), with a fractional part only where the
milliseconds are not zero.

**A text field is printed from `text`.** It arrives as `{id: "text", text,
markdownText}`, and `markdownText` holds HTML of the same text — the server's way of
showing it to a human, like a user's full name and a period's `presentation`. The text
is the identity, and it is printed as prose (ADR-0003); the HTML is asked for by nothing
and printed by nothing.

Where the permitted values live is likewise a function of the type, not one field of
a bundle: `values` for enum, state, version, build and ownedField; `aggregatedUsers`
for user; nothing at all for group, text, period, date, date and time, integer, float
and string, whose fields take whatever the type itself allows. An empty source reads
the same way — a user field with no `aggregatedUsers` takes any user, a banned one
included — so it is no restriction rather than no one.
On a working instance `Assignee`'s `bundle.values` holds a few users
and groups, while many times more users are actually permitted — reading it as the answer
would be the same class of error as the one above, arrived at by analogy from a
branch that happened to be checked.

**A type of work is no custom field, and its permitted values live nowhere near a bundle.**
They are a setting of the project — `plugins.timeTrackingSettings.workItemTypes` — and
`ytrack` reads them off the issue a write is about, in the one request that also settles the
readable id the write is addressed by. The global catalogue of the instance is not that set:
it holds 17 types where DEV writes against 15, and a name taken from it is `unknown_name`
before anything is sent, while sending it anyway is `400 Выбранный тип работы не
поддерживается в этом проекте YouTrack`. Reading the set off the project rather than the
catalogue costs nothing it did not already cost, and the two are far enough apart to be
noticed: measured on DEV, the 15 names of its types meet none of the 34 names its 28 fields
go by — `name` and `localizedName` both — and none of the 92 values of their bundles, letter
case aside. **A project with time tracking switched off still lists them**: DOCS answers
`enabled: false` and 16 types, so the flag says whether work items may be written and the set
says which types exist, and neither answers for the other.

## What follows from it

- **Multiplicity on input replaces the set.** A REST array is a whole-set write; add
  and remove would be read-modify-write, which has no defined result under
  concurrency. The consumer composes them, as the map requires of every aggregate.
- **Field names are resolved locally**, against `name` and `localizedName`,
  case-insensitively, per project — not out of politeness, but because YouTrack
  answers an unknown field name with `500 server_error`, which every retry policy will
  read as an outage and repeat forever.
- **The names of values are resolved by the server, and letter case is nobody's
  business.** A field name is ours to resolve and a value name is not: the bundle of one
  field may hold hundreds of values, and the server matches a name
  case-insensitively anyway — `task` is `Task`, `ревью` is `Ревью`, `development team`
  is `DEVELOPMENT Team`, `ADMIN` is `admin`. So a value goes out as it was typed and
  comes back canonical, and the comparison after the write holds the two to each other
  letter case aside. A value the server does not know is `400` in the server's own
  words, which is a truer answer than a nearest name of ours would be: the value that
  differs from a bundle's by one Cyrillic letter is refused with a message naming a
  string indistinguishable from what was typed.
- **Mandatory fields are checked locally** and reported all at once. DEV has six, and
  the server names one per round trip. Two of the six are never asked of a caller: a
  field the project fills unasked is filled by the server — `Priority` becomes `Low` on
  DEV and DEMO files an issue given nothing at all with `Normal`, `Bug` and `To do` —
  and a field a condition hides does not stand on the issue being filed at all. What is
  required is required of a **creation**: an issue filed before its project required a
  field holds it empty to this day — DEV-2 of the polygon is one — so an update checks
  only the fields it empties.
- **A field a condition hides is refused before a creation and left to the server on an
  update**, because the server treats the two differently and only one of them is
  silent. On an update it answers `400 invalid_properties` in its own words, and it
  writes the field where the same body gives the watched field a value that shows it —
  so ytrack evaluates nothing there. On a creation it files the issue under a `200` and
  drops the value, which is the shape of wrongness this ADR exists against: the
  condition is therefore evaluated against the body about to go out, and a field the
  body's own values leave hidden is refused before the request.
- **A write re-reads itself and compares only the fields it wrote, by identity.**
  Echoing the whole body is impossible: a write moves neighbouring fields. Without the
  comparison, `{"id":"PT7H"}` reports success and empties the field.
- **`readOnly` in the specification says nothing about what a write sends.**
  `Issue.customFields` and an issue field's `name`, `value` and `$type` are all marked
  read-only, and all four are exactly what a body is built from — the server refuses a
  body without `$type` outright. The specification is read for the shape of a value,
  never for the permission to send one.
- **`/api/commands` is not used.** It adds to multi-value fields atomically and speaks
  localized names, but it cannot create an issue, cannot clear a field, and answers
  `Priority Low Medium` with two commands and no error at all. A second way to name
  the same field inside one tool is a mechanism for being quietly wrong. It is out of
  reach rather than merely unused: the generated surface is written by the overlay,
  which carries no entry for it, and the adapter file — the one place the generated
  package is imported, asserted by `make ytapi` — calls nothing of the sort.
- **The fields of a project are printed by `ordinal`, and fields of one `ordinal` in the order the
  server sent them in** — the order their attachments were given ids in. On a project nobody ever
  ordered that tie is the whole order rather than an edge of it.
- **The metadata cache may confirm but never refuse**, below.

## The cache confirms and never refuses

The metadata of a project is kept on disk between calls, under `<HOME>/.ytrack/cache/`:
a directory per identity, named by the SHA-256 of the canonical address of the API and
the token, and in it a file per refreshing request, named by the SHA-256 of that
request's path and query. The identity is the key because both the fields a token is
sent and the values of a bundle may be filtered by permission, so what one token was
told answers nobody else; the request is the file name because an expression that grows
in a later version leaves what was written under the old one unreachable rather than
quietly stale. Directories are `0700` and files `0600`, no token is written into a file
— only metadata — and nothing is synced: what a crash leaves half-written is a file that
does not read back.

The metadata is written the moment it arrives, before a name is resolved against it
and before an id is held to the form a path needs, so a run that ends in a refusal
leaves the cache warm all the same. The two wants pull apart here: a cache warm after
a refusal, against nothing unusable ever reaching the disk. The first wins, because
the cache is not a judge. An id of a shape no path can hold is caught where it is
about to be used — by the one check both paths walk — and sieving the metadata on the
way in would put a second judge in the one place that must never decide anything.

The warm path and the fresh path are one walk of the same steps: the name resolved, the
id held to the form a path takes, what to print settled against what the field holds,
the field asked for by that id, the answer confirmed against the naming. What the
metadata came from decides only the disposal of a step that does not carry through, so
the two paths cannot drift apart over what the steps are.

What the cache may do is spare a request. What it may not do is answer. A cache behind
the server shows itself as one of: the name resolves to no field, an id is not of the
form a path needs, the type is outside the table, the request for the field comes back
`404` or `upstream_lied`, or the field that arrives disagrees with what was resolved.
Each of these reads the metadata again and goes on the ordinary fresh way, so a refusal
— `unknown_name` above all — is reached only after a refresh in that same process. A
refusal over the grammar of `--fields` is not a miss: it is the caller's own, and
reading the metadata again would not mend it. `denied`, `rejected`, a transport failure
and a 5xx pass outwards as they are.

A cache that could not be written changes nothing and is not reported. That is not an
error swallowed: the cache cannot change the result of a command, and the vocabulary of
codes has none for something that did not happen and was not asked for. Only `field
show` reads and writes it. `field list` resolves no name, so it neither reads nor
writes.

## Consequences

Every write needs metadata. On update it costs nothing beyond the read the update needs
anyway: one request brings `idReadable`, the class of every field on the issue and the
project's whole metadata under `project(…)`, about 15 KB. On create it is one request,
6 502 B on the polygon, and a full warm-up across all visible projects is one more.

**A write reads the metadata afresh and never off the cache.** The cache may confirm and
never refuse, and a write is the one caller with nothing to confirm it against: a class,
a requirement or a condition out of date turns into a refusal ytrack invented or into a
field the server drops in silence, and both would be ytrack's word about a server it had
not asked. `field show` is still the only reader and writer of the cache.

Twenty field types are modelled, and the dev polygon now carries a field of every one
of them: the rows that used to be assertions — `build`, `ownedField`, `group`, `text`
and `float` among them — are measurements, one contract test each, sent against the
instance by `field show DEV "<the field of that row>"`. The column this ADR spends most
of its words on is measured too, on every row: one issue is filed with a value of each of
the twenty types and then updated over all twenty, and the class of each field agrees
between the body the table built and the body the server's own answer built. What is
still unwitnessed is one string of one branch: `StateMachineIssueCustomField` is in the
specification, it has been met on neither instance — the polygon answers
`StateIssueCustomField` for `State` — and a field a state machine governs is the only
field whose table row nothing has stood beside.
