---
status: accepted, partly implemented
---

# Partial responses are trees, not generated structs

_The read side is built on projects: the caller owns `fields=`, the default is
named in `--help` and holds only what every reader is sent, syntax is refused before
the request, and names are judged against the answer. The write side arrives with
the first command that writes._

Every YouTrack read is shaped by its `fields=` parameter: the same `Issue` schema
arrives with three fields or with thirty, and nothing in the payload says which
of the two an absent key means — "I did not ask for it" or "it is empty".
`youtrack-cli` died on exactly this distinction. Unable to tell the two apart, it
substituted a fallback where it owed an honest error, and that single habit is
three of its defects.

The decision is that `ytrack` never faces the question, because it never throws
away the answer: **the request is the record of what was asked**. The caller owns
the field set, the response is decoded into a tree that preserves the shape it
arrived in, and request bodies are built the same way. "Not requested" is not a
state of the data at all — it is a property of the call.

## What the server actually does

Measured against a working instance (YouTrack 2026.1, project DEV), not read off
the specification:

| Probe | Result |
|---|---|
| `fields=idReadable,resolved` on an unresolved issue | `"resolved": null` — a requested-but-empty scalar is emitted explicitly |
| `fields=…comments(id)` on an issue with none | `"comments": []` — a requested-but-empty collection likewise |
| no `fields=` at all | `{"id":…,"$type":"Issue"}` — the server's own default is the id and nothing else |
| `fields=*` | `400` — there is no wildcard |
| `fields=idReadable,totallyBogusField` | `200`, key silently absent |
| `fields=idReadable,shortName` (valid on `Project`, not `Issue`) | `200`, silently absent |
| `fields=idReadable,project((` | `400 {"error":"bad_request",…}` — syntax is checked, names are not |
| `parent(issues(parent(…)))` nested 20 deep | `200` — no depth limit; recursion ends when the data does |
| the same field named 300 times | `200`, deduplicated |

So a requested field is emitted only on an object whose type declares it: a key one
subtype declares is absent from the objects of its siblings, as
[ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md) measured. Nor is it
always emitted there. A field the caller's rights hide does not arrive either: on the
dev instance `admin/projects/DEV?fields=shortName,name,archived,leader(login),createdBy(login)`
answers the admin with all five keys and a user holding only `project-read-basic` with
`shortName` and `name` alone. Absence means "not requested", "not a field of this
object's type", "hidden from this caller" — or "the name was wrong and YouTrack said
nothing". The second and the last leave the same trace, and telling them apart is ours
rather than the server's: ADR-0007 does it by the `$type` the server named. A field
hidden by rights leaves the trace of a field the server did not send, and one answer
does not tell those two apart.

Further behaviours were measured on the write path:

| Probe | Result |
|---|---|
| body omits a custom field | `200`, the field keeps its value — untouched |
| body sends `"value": null` | `200`, the field is **cleared** |
| custom field without `$type` | `400 {"error":"Bad Request","error_description":"$type is required"}` |
| `Статус разработки` set to a value | `200` — and `Статус анализа`, which nothing wrote to, changed with it through a workflow on the instance |
| `readSharingSettings`, `updateSharingSettings` and `tagSharingSettings` in the body of a tag | `200`, and each of the three comes back holding the groups that went out — the specification marks all three `readOnly: true` |
| `visibleFor`, the writable member offered in their place | `{"name":…}` is `400 Чтобы найти сущность типа UserGroup, укажите ее ID`; `{"id":…}` is taken and lands in `readSharingSettings.permittedGroups`; read back it is **one** group of that set, and `null` where the tag is shared with a person alone |

`$type` is therefore mandatory in request bodies while the specification marks it
`readOnly: true` in all 95 schemas that declare it. The specification is wrong,
and this is now measured rather than inferred from JetBrains' documentation.

**The same marking is wrong on the three sets a tag is shared by, and there the
writable member the specification does offer cannot say what they say.** All three
were written live against the polygon — the body of `tag create` naming a group in
each, the answer coming back with it — while `visibleFor` carries one group and
reads back `null` over a tag shared with a person, so a tag shared with two groups
is inexpressible through it and a tag shared through it reads as shared with fewer.
`tagSharingSettings` has no counterpart at all: it is the one member that grants the
right to hang the tag on an issue, and nothing outside the three says who holds it.
So the flags of `tag create` write the sets, and a `readOnly` marking is read here
the way `$type` taught this ADR to read one — as a claim to hold against the server
rather than a rule to obey.

## Why the generated structs cannot carry this

The first generated client has 198 fields of type `nullable.Nullable[T]`,
which holds three states, and 2219 plain pointers, which hold two. The split is
not a judgement about which fields need it. Of the 164 properties the spec marks
`nullable`, 100 are inline and survive generation; the other 64 are a `$ref` with
a sibling `nullable: true`, which OAS 3.0 discards — so `draftOwner` and
`externalIssue`, both of which return real `null`, arrive as plain `*User` and
`*ExternalIssue`. A further 472 properties are not marked at all.

On the read path that would reintroduce the exact confusion this ADR exists to
remove. On the write path it is worse than confusing: `*T` with `omitempty`
marshals `nil` to an absent key, so a generated struct **cannot emit `null` at
all**, and a field can never be cleared. The custom-field `value` slot compounds
it — the same key is an object, an array or `null` depending on `$type`, which is
why the first generated client already had to force `json.RawMessage` onto six of them.

## Considered options

- **Patch the spec so every writable property becomes three-state.** Restores the
  ability to clear a field, at the cost of `nullable.Nullable[T]` across the whole
  client, and still leaves bodies that omit the mandatory `$type` and cannot type
  the `value` slot. Solves one of three problems.
- **Repair only the 64 `$ref` + `nullable` properties.** Cheaper, and restores
  what the spec already claims. But the spec's `nullable` marking is not the true
  set of clearable fields — that was measured on a property the spec does not mark
  nullable at all — so the repair would be principled and still wrong.
- **Build request bodies as trees, symmetrically with responses.** Chosen. An
  explicit `null` is an explicit `null`, `$type` is set because we set it, and the
  `value` slot needs no type because nothing pretends to type it.

## What follows from it

- The caller owns `fields=`. Each command carries a **default** expression — a
  recommendation, printed in `--help`, not a hardwired set. Bare `--fields`
  replaces it; `--fields +comments(text)` extends it.
- There is no synthesized "everything". The schema is recursive with no natural
  bound, so any depth `ytrack` picked would be its own invention. The generous
  default is the one-request answer instead; `wikifiedDescription` stays out of it,
  being `description` re-rendered as HTML and therefore paid for twice.
- **A default holds only what every reader of the entity is sent.** A field rights may
  hide is asked for by name: YouTrack sends a user holding only `project-read-basic`
  neither `archived` nor `leader` of a project, so `project show` and `project list`
  default to `shortName,name`, and `--fields +archived,leader(login)` asks for the
  rest. A hidden field leaves the trace of one the server did not send, so a default
  naming it would refuse such a reader with `upstream_lied`: the outcome would depend
  on who asked, as a default naming `State` would make the shape depend on which
  project was asked ([ADR-0003](0003-all-output-is-one-yaml-document.md)). A contract
  scenario holds each default to such a reader, the polygon's member. Where the entity
  may belong to somebody else, that scenario has to reach one that does: the default of
  `tag list` names `readSharingSettings`, and a tag of the member's own would say
  nothing about whether rights hide that key on a tag of somebody else's — so the member
  is shown a tag the admin shared with the group they stand in, and the record is held to
  the three keys of the default and no fourth. All three arrive, so the default stands as
  written.
- **Syntax is validated before the request**, for a precise error at no round
  trip. **Names are judged after it**, against the real response, by walking the
  requested tree and stopping wherever the parent came back `null` or `[]` — a flat
  comparison of leaves reports false failures on exactly those two.
- A single bad field name **fails the whole command**: empty stdout, machine code
  and text on stderr, non-zero exit. The consumer is a pipe, and a partial record
  on stdout is processed silently by whatever comes next.
- The error carries the machine code, the field that did not arrive, the nearest
  valid names for that schema, and the `fields=` expression actually sent — so the
  retry is one turn and correct. An agent that has to guess spends another turn,
  and a turn costs far more than a verbose error.
- Writes re-read after themselves and return the same generous default. Writes
  cascade — measured — and an agent that cannot see what moved will go and look,
  which is that same extra turn.
- `$type` is printed where it discriminates and dropped where it does not. In a
  full issue it is about a third of the response: the occurrences on custom fields name
  which is single, multi or state and are the only place that is written; the rest
  restate the position they already sit in — `"$type":"Issue"` on the issue you asked
  for.

## Consequences

The generated client keeps its operation surface — 136 paths, parameters,
authentication — and loses its bodies. That is a smaller client than was assumed
when `oapi-codegen` was chosen, and it does not change that choice: the generator was
picked for what its hand-written part would cost
([ADR-0004](0004-the-generator-owns-the-operation-surface.md)), and this makes that
part larger and more deliberate rather than different in kind. Where exactly the seam
falls is the package-boundary decision
([ADR-0006](0006-a-package-boundary-needs-a-second-importer.md)), which depends on this
one.

The cost paid is compile-time safety on bodies. A tree accepts a misspelled field
name that a struct would have rejected, and Go will not catch it. That weight
moves onto the contract tests against a live instance, which the map already
requires for every hand-written correction — a heavier reason to build the dev
instance (`dev/`) than existed before this ADR.

This overrides the map's output line, which read that subtasks are always in the
frontmatter. A fixed set of fields is what the caller now chooses, and the output lays
out whatever arrived — as one YAML document, since
[ADR-0003](0003-all-output-is-one-yaml-document.md) retired the frontmatter.
