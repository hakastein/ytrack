---
status: accepted
supersedes: in ADR-0003, the bullet "A record of the history is one change, and five of its seven keys are
  values of ytrack's own"; in ADR-0006, the rule that the category row says what target, added and removed
  print as; in ADR-0007, the section "A name below a value ytrack reads itself is judged by the rule that
  prints it" as far as target, added and removed go; in ADR-0009, "nothing at all where the word is the value
  of a flag"
---

# Journal values are trees, and added and removed are always a list

ytrack printed the history of an issue as `issue-history list --query <search>`, and it printed
every value under `added` and `removed` by **one name its category gives one**: `idReadable` for an
issue, `id` for a comment, an attachment and a work item, `login` for a user, `name` for a value of a
bundle or a tag. The list or the scalar the server sent was kept, and so was its `null`. A name written
under either key was `bad_usage` before any request
([ADR-0003](0003-all-output-is-one-yaml-document.md)), and the names ytrack asked there carried a mark
that took them out of the catalogue's judgment
([ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md)).

A commit does not fit that rule. The polygon has no VCS integration and no API files a commit without one,
so the commits were measured on a working instance with a VCS integration:

| Probe | Result |
|---|---|
| `GET /issues/<issue>/activities?categories=VcsChangeCategory` | a `VcsChangeActivityItem` per commit, each with one `VcsChange` under `added` and `[]` under `removed`, `field: null` |
| what a `VcsChange` holds | `id`, `version` (the hash), `urls` (a list of one link to the commit), `text` (the message, several lines), `date`, `author`, `files` always `-1`, the `processor` of the integration |
| the author of such a record | a `User` where YouTrack matched the committer to one; a `VcsUnresolvedUser` with login `system_user@` where it matched nobody |
| the `target` of such a record | the `VcsChange` itself, with no readable id of any issue |
| a merge request | no entity: `/issues/{id}/pullRequests` answers `404` and the specification has no schema of one. A merged request is its merge commit, whose text says `See merge request …` |
| `POST /issues/{id}/vcsChanges` on the polygon | `Entity is not saved` |

No one name describes a commit: a link without the message says nothing, a message without the link
cannot be followed. And it was the second category the rule could not read — a change of a custom field
was already read by the type of the field rather than by its category.

The decision is five rules:

- **`issue-history` is `activity`, and its one verb takes the issue as its owner**:
  `ytrack activity list <issue> [--category …] [--fields …] [--limit …]`, sent to
  `GET /issues/{id}/activities`. The readable id of an article and an internal id are refused before any
  request, as they are wherever an issue alone is named; YouTrack keeps no journal of an article. `--query`,
  the markup of the search and the warning of free text go with the old command.
- **`added` and `removed` are always a list.** `null` prints `[]`, a scalar or one object a list of one.
- **An item of the list is a scalar where the value is one and an ordinary tree otherwise.** A moment, a
  day, a duration, a number and a text are written as ytrack writes them anywhere — a moment and a day in
  ISO 8601, a duration as the ISO period of its minutes — and every object is printed by the names asked
  under `added` and `removed` in `--fields`, by the `$type` the server named on it, like any other tree.
- **The default is `added(id,idReadable,login,name,urls)`**, and the same under `removed`: the id, which is
  the only name a comment and a work item have, and whichever of the four names a type has. A heavy name —
  the `text` of a comment or of a commit — is asked for by name: `--fields +added(text)`.
- **`target` is neither printed nor asked for.** The caller named the issue, and for a commit the target is
  not the issue at all.

`category` stays the identifier YouTrack keeps it under and `field` the name of what the change was of —
the name the project gave a custom field, or the untranslated phrase of a link in the direction of the
issue — and a name under either of the two is still `bad_usage`. `VcsChangeCategory` joins the table of
categories and the default set, the one row held to a working instance rather than to the polygon.

## The family under added

Taken out from under the mark, `added` and `removed` are judged by the catalogue, and the catalogue reads
them well: sixteen of the 27 schemas of the hierarchy of `ActivityItem` declare a schema there —
`[]VcsChange`, `[]IssueComment`, `DurationValue`, `[]Tag` and so on — so the family of the place is their
union whatever arrived. The other eleven declare an object of no schema or a scalar: the parents that stand
for no change of their own, and the changes of a custom field, of a text, of a simple value and of a
resolution. Of the categories of the table, the one that holds objects there is the change of a custom
field, and what it holds is a user, a group or a value of a bundle. The first two are in the union already;
values of a bundle are not, and a family read off what arrived would hold them on the journal of a state
field and not on a journal of links. So the place names `BundleElement` itself, as a schema that may stand
there beside what the specification declares, and the family is the same for every journal: 26 schemas and
103 names.

[ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md) measured the values that arrive under
`added` on the polygon as seven schemas — `Issue`, `IssueComment`,
`IssueAttachment`, `IssueWorkItem`, `StateBundleElement`, `Tag`, `User` — beside bare numbers, texts and
`null`; an instance with a VCS integration adds `VcsChange`. A name one of them declares and another does not is left out of the
other, so `added(login,idReadable)` over a mixed journal prints the login of a user, the readable id of an
issue and nothing of a comment, and refuses nothing.

A name no schema of the family declares is `unknown_name` with the nearest names, **before any request**.
The judgment of the answer reads the values that arrived and only those, and `removed` is `[]` on most
records: judged after the answer, `removed(idReadabel)` would pass a journal of filings and read as values
that carry nothing.

The change of a custom field keeps one thing of the old rule. Its values arrive bare where the type of the
field is one ytrack reads itself — 90 for a period of 90 minutes, `1764116587829` for a date — and the
specification says nothing of what such a number is, so the type of the field the record names is still
read, and still says whether its values arrive as objects or as scalars. A value of the other shape is
`upstream_lied`.

A value of a change of the duration of a work item is a `DurationValue`, printed out of its minutes, which
do not arrive unless asked for. The request asks `minutes` under both keys on ytrack's behalf; on every
value of another type the family explains the absence and it is left out.

## Completion offers the categories

[ADR-0009](0009-completion-is-answered-by-ytrack-not-by-cobra.md) offered nothing where the word being
completed is the value of a flag, since ytrack knew no value of any flag it takes. It knows one now: the
categories are ytrack's own writing, and offering them costs no request. A flag whose values are a closed
set of the binary's says so in an annotation, `ytrack.completesClosedSet`, and the protocol offers the set
there, in `--category Vcs` and in `--category=Vcs` alike. Every other value of a flag is still the server's
to know and is still offered nothing.

## Considered options

- **One name per value, chosen by the category, with a commit named by its link.** It keeps a line short
  and it is the rule a commit already broke: the message is the reason to read a commit at all, and a
  second rule for it would be the third exception of one table.
- **A value printed by the names the caller writes and nothing by default.** A journal of `[{}]` under
  every record reads as nothing having changed.
- **`added` and `removed` as the server sent them — a list, a scalar or `null`.** Every reader would have to
  branch on the shape, and the shape follows the subtype of the record, which the document does not print.
- **The family read off what arrived, as at every other place of no schema.** A name of a value of a bundle
  would be `unknown_name` on a journal of links and fine on a journal of states.
- **A separate entity for commits.** A commit has no verb of its own here — ytrack writes none — and in the
  journal it keeps its place in the chronology of the issue.
- **A journal of the issues of a search**, the old `--query`. It was the one entity of the tree that took a
  selection rather than an owner; the search language already answers which issues changed, and a journal
  of one of them answers how.

## Consequences

The journal of every category prints one way: a list under `added` and `removed`, scalars where the value
is one, trees where it is an object. A record of a comment or of a work item prints its value as `{id: …}`,
where it used to print the bare id, and a caller who wants more of it writes the names.

The mark ADR-0007 put on the names ytrack reads by a rule of its own remains on one place, `field`: the
subtypes of a filter the server sends there are still absent from the specification.

The judgment of names gains one input: a requested name may carry schemas that stand at its place beside
the ones the specification declares. `added` and `removed` of a journal are the only place that uses it.

`VcsChangeCategory` is held by records taken from a working instance, in the tests of the stub server,
since no contract can record a commit the polygon cannot file. When the polygon gets a VCS integration, the
row moves to the contract with the rest.
