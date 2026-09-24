---
status: accepted
superseded in part by: ADR-0011, which takes target, added and removed out of the rule that prints a
  value of ytrack's own
---

# A name is judged by the type the server named

[ADR-0001](0001-partial-responses-are-trees-not-generated-structs.md) made the request the record of what
was asked and put the judgment of names after the request, against the answer.
[ADR-0005](0005-failure-is-a-document-and-nothing-unjudged-is-printed.md) gave that judgment two codes —
`unknown_name` for a name that did not resolve locally, `upstream_lied` for a key that was asked for and
did not arrive — and [ADR-0006](0006-a-package-boundary-needs-a-second-importer.md) left open which of
the two a name the server drops in silence is refused with. The answer itself does not say: under `200`,
a misspelled name and a name the object's type does not have leave the same trace, a key that is not
there.

The decision is that **an absent key is judged by the `$type` the server named on its object, against a
catalogue of the schemas of the vendored specification**. Where the named type may stand at the object's
place, a key it declares is `upstream_lied` and a key only another schema that may stand there declares is left
out of the node; where it may not, or no type was named, a key a schema that may stand there declares is
`upstream_lied`. A key no such schema declares is `unknown_name`, carrying the nearest names and the `fields=`
that went out.

## What the server actually does

Measured on the dev instance (YouTrack 2026.1.13757), not read off the specification:

| Probe | Result |
|---|---|
| `issues/DEV-1?fields=idReadable,totallyBogusField` | `200`, the key silently absent |
| `admin/projects/DEV?fields=shortName,bogus,leader(logn)` | `200`, both names silently absent; `leader` arrives holding only its `$type` |
| `issues/DEV-1?fields=customFields($type,name,value(login))` | the values of the enum, state and period fields arrive with no `login` key at all, not with `login: null` |
| `admin/projects/DEV/customFields?fields=$type,field(name),bundle(id)` | `bundle` on the eighteen enum, state, version, owned, build and user fields; absent on the ten period, text, group and simple ones |
| `admin/projects?fields=shortName,customFields($type)` | the fields of DEV are of ten subtypes of `ProjectCustomField`, of DOCS four, of DEMO seven |
| `admin/projects/DEV?fields=shortName(foo)` | `200`, `shortName` whole: a sub-selection of a scalar is ignored |
| `admin/projects/DEV?fields=createdBy(bogus)` | `createdBy: null`, and nothing under it to judge |
| `customFields($type,name,value)` on DEV-8 | the date and simple fields' values arrive as `1789560000000`, `42`, `1.5` and `"EXT-1"`, where the specification declares an object |
| the `$type` under `field` of an activity, over the 66 activities of the polygon | `CustomFilterField` on 4 and `PredefinedFilterField` on 44, both declared; `LinkTypeFilterField` on all 10 records of a link and `WorkItemFilterField` on all 8 of a work item, **neither of them among the 218 schemas**. `CommentReactionActivityItem` is a third such name on a working instance |
| the schemas that may stand under `added` of an activity, on the polygon | seven — `Issue`, `IssueComment`, `IssueAttachment`, `IssueWorkItem`, `StateBundleElement`, `Tag`, `User` — and a bare number, text or `null` besides. Of the four members an identity is read by, **no schema declares more than two**: `Issue` has `id` and `idReadable`, `User` `id` and `login`, the rest `id` and at most `name` |

ADR-0001 measured that a recognized requested field is always emitted. It is only on the objects whose type
has it. A key one subtype declares is absent from the objects of its siblings, so judged by the letter of
ADR-0001 the expression ADR-0003 praised for reading every custom field in one request, `value(name,login)`,
would be refused on every issue, and `bundle` on every project.

What the specification holds, read off `api/openapi.json`:

| Fact | What follows |
|---|---|
| 218 schemas, 996 properties they declare themselves, 348 names | small enough to carry: the catalogue is 45 KB of Go |
| 123 schemas extend exactly one other through `allOf` | the ancestors of a schema are a chain, its descendants a tree |
| 143 schemas stand in 20 hierarchies of two or more, the widest `ActivityItem` with 27. Taken whole rather than from a schema down, a hierarchy adds names to 78 of them: nine to `Project`, `owner` among them, fifteen to `SavedQuery`, `bundle` and `defaultValues` to `PeriodProjectCustomField` | what the family of a place of no schema costs |
| 95 discriminators, every one on `$type`. The 20 at the root of a hierarchy map each schema of it, two otherwise than the schema itself: `$type` `SingleValueIssueCustomField` is the schema `DatabaseSingleValueIssueCustomField`. The other 75 map nothing, and the server writes those schemas' own names | the catalogue keys a schema by what the server writes in `$type` |
| 18 properties hold an object of no schema, `Project.customFields` and `IssueCustomField.value` among them | where the specification names no schema, the answer has to |
| `ProjectTeam` declares no `users` and `User` no `name`; the server sends both | the specification lags the server, so nothing that arrived is refused for being absent from it |

## Four outcomes

For a key K absent from an object at a place, where T is the `$type` the server named on the object:

| | Verdict |
|---|---|
| T is a schema of the family of the place and declares K, itself or through a schema it extends | `upstream_lied` — the type has the field, and it was not sent |
| T is a schema of the family and does not declare K, while another schema of the family does | not a refusal — K is left out of this object's node |
| T is not a schema of the family — not in the catalogue, not named, or a schema the catalogue has that may not stand at the place — while a schema of the family declares K | `upstream_lied` — nothing shows that K does not apply |
| no schema of the family declares K | `unknown_name` — the name did not resolve, whatever T declares |

A schema the catalogue has vouches for nothing where it may not stand: an object the server named `User`
where a project stands, or `Project` under `leader`, is refused for the keys it lacks rather than printed
without them. Where one answer holds both kinds, the refusal is `upstream_lied`: fixing a name does not bring a field
the server did not send. Each field is listed once, however many objects lack it, in the order of the walk.

ADR-0005's table stands as written. `unknown_name` is still a name that did not resolve locally — resolved
against the catalogue once the answer has named the types — and a misspelled name is `unknown_name`.

## The place and its family

A **place** is where values stand in the answer: its root, or a field asked of the values at the place
above, every item of a list standing at the place of the list. The **family** of a place is the schemas an
object there may be of:

- at the root, the schema the operation answers with and its descendants. The command names it — `Project`
  for `project show`, and `[]Project` for `project list`, whose answer is a list of objects with every record
  standing at the root — because the catalogue holds schemas, not operations;
- below, what the family above declares the field to hold, each schema with its descendants;
- where a schema of the family above declares the field an object of no schema or a scalar, or none declares
  the field at all, every schema of each hierarchy the server named at the place, a hierarchy being a schema
  that extends none with all its descendants. Where some schemas above declare a schema for the field and
  others an object of no schema or a scalar, the family holds both. The family is the same whichever schemas of a hierarchy arrived and in
  whatever order: a field period fields lack and enum fields have is left out on a project of period fields
  alone as on one that has both. A later object may name another hierarchy, so no name is judged before the
  whole answer has been walked.

A sub-selection of a scalar is judged as names absent from an object that named no type: `shortName(foo)`
is `unknown_name` with no nearest names, and `leader(login)` over `leader: "admin"` is `upstream_lied`. The
exception is a field the family above declares a scalar or an object of no schema — how the specification
writes a value the server sends as a scalar. There the scalar is printed whole, as ADR-0002 measured for
`value(name)` over a list of mixed custom fields, and only a name no schema of the place declares is refused.

## A custom field's name is judged by another catalogue

The names a caller writes under `customFields` of an issue —
`--fields 'customFields(State,"Статус разработки")'` — are not properties of any schema,
so this catalogue cannot judge them and never sees them. What goes out in their place is
the tool's own expression,
`customFields(name,value(…),projectCustomField(id,ordinal,field(fieldType(valueType,isMultiValue))))`,
and the names travel beside it as parameters of the request
([ADR-0004](0004-the-generator-owns-the-operation-surface.md)). The judgment above walks
that expression, so `State` is never weighed as a property of `IssueCustomField`.

They are judged instead against **the catalogue of custom fields of the instance**, read
from `/api/admin/customFieldSettings/customFields?fields=name,localizedName&$top=-1` —
31 fields and 2 509 B on the polygon — once per call, and only where the caller named at
least one. The match is the server's own: letter case aside, against `name` first and,
where that answers nothing, against `localizedName`. One match yields the canonical
`name`, which is what is sent and what is printed as the key, and two names of one field
collapse into the one key, where the first of them stood. No match is `unknown_name`
carrying `nearest` by the same rule as above; more than one match is `unknown_name`
carrying `candidates` instead — no pair of names collides on the polygon, and no other
instance was measured. A `403` on the catalogue is `denied`, which is what a token
with no role gets, and such a token is shown no issue either.

The catalogue of the **instance** and not of the project, on two measurements. A token
with no role on a project is sent that project's field list as `200 []` — both off the
issue, as `project(customFields(id))`, and off `/api/admin/projects/DEV/customFields` —
so every name would resolve to nothing for exactly the readers the default is built to
serve, while the instance's catalogue reads the same for a member as for an admin. And a
selection may cross projects, where whether a name is a name would otherwise depend on
which projects the selection happened to reach.

Names that reached the expression through the default alone are not resolved. The
default pays for no request, and a reader would otherwise be refused over a name they
never wrote.

## A name below a value `ytrack` reads itself is judged by the rule that prints it

_Superseded in part by [ADR-0011](0011-journal-values-are-trees-and-always-a-list.md): `target` is no
longer asked for, and `added` and `removed` are judged by the catalogue over a family of every schema a
record declares there and every value of a bundle. The mark stays on `field` alone. The measurement of
the schemas under `added` in the table above stands; that no one of them declares all four members an
identity is read by is now why the default asks for all four, each value printing those its type has._

Five places of a record of a history reach the document as nothing like what arrived:
`target` as the readable id of the issue the change belongs to, `category` as the
identifier YouTrack keeps it under, `field` as the name of what the change was of, and
`added` and `removed` as the values, each by the one name its category gives one. The
caller writes none of the names below those five — one written there is `bad_usage` before
any request — so what goes out under them is the tool's own expression, exactly as it is
under `customFields` above.

**On three of the five the catalogue is told to stay out, and a measurement puts it there
rather than symmetry.** `ActivityItem.field` is declared a `FilterField`, a family of three
schemas; the server names `LinkTypeFilterField` on all ten records of a link of the polygon
and `WorkItemFilterField` on all eight of a work item, and the specification has neither.
That is the third outcome of the table above at its sharpest — a type the family does not
have, while a schema of the family declares `customField` — so every history holding a link
or an hour of work would be refused `upstream_lied` over a key those records have no
business carrying. `added` and `removed` fail the same way from the other side: both are
declared an object of no schema, so their family is whatever hierarchies happened to
arrive, and the four members an identity may be read by are spread over seven schemas that
declare two of them at most. A selection of links alone puts `Issue` there and nothing
else, and `name` and `login` are then declared by no schema of the place at all.

So a requested name carries a mark saying `ytrack` reads what stands under it by a rule of
its own and prints one value for it. The walk does not descend past such a name, and what
stands there is held to that rule instead — which refuses harder than the catalogue would,
naming the category of the record and what a change of that category holds. The mark is the
tool's: no expression of a caller's can put it on, and where two spellings of one name are
merged it joins the way the caller's own mark does.

**`target` and `category` keep the catalogue.** It judges them correctly, because every
subtype of an activity that has a target declares it, so the family at that place is their
union — and a comment arriving without the issue it belongs to is caught there as a key
withheld, before the rule that prints the target ever looks at it. A mark on those two
would buy nothing and would take that refusal away.

## The refusal

```yaml
code: "unknown_name"
message: "the names under unknown are not declared where they were asked for"
request: "GET http://localhost:8091/api/admin/projects/DEV?fields=shortName,bogus,leader(logn)"
fields: "shortName,bogus,leader(logn)"
unknown:
  - {field: "bogus", nearest: ["$type", "archived", "createdBy", "customFields", "description", "fromEmail", "iconUrl", "id", "issues", "leader", "name", "replyToEmail", "shortName", "startingNumber", "team", "template"]}
  - {field: "leader(logn)", nearest: ["login"]}
```

An `upstream_lied` of the judgment carries `missing: [{field, type}]` instead, `type` being what the
server named in `$type` and `null` where it named nothing. A field is written in the syntax of `fields=`.
The nearest names are the family's names within two edits, letter case aside — five at most, nearest
first, then by name — and, where none is that near, every name of the family in order.

A field the caller's rights hide does not arrive either, and one answer does not tell it from a field the
server did not send ([ADR-0001](0001-partial-responses-are-trees-not-generated-structs.md)). So the message of the
refusal names rights as a possible cause — here the polygon's member asking for `archived`:

```yaml
code: "upstream_lied"
message: "the fields under missing were asked for and did not arrive: the caller's rights may hide them"
request: "GET http://localhost:8091/api/admin/projects/DEV?fields=shortName,name,archived"
fields: "shortName,name,archived"
missing:
  - {field: "archived", type: "Project"}
```

The code stays `upstream_lied`: the same call with the same token brings nothing more, which is what ADR-0005
promises of the code, and whether to change the token or drop the field is the caller's to decide. No default asks
for a field rights may hide (ADR-0001), so only a caller who named one meets this refusal for it.

## The catalogue is generated

`scripts/catalogue.go`, on the standard library alone, reads `api/openapi.json` and writes
`internal/youtrack/catalogue.gen.go`: one function returning a map from each schema, under its `$type`, to
the schema it extends and what each property it declares holds — a scalar, an object of a named schema,
an object of no schema, or a list of scalars or of objects of a named schema. The map is built when the
function is called, and the passage calls it only when a key is absent or a scalar was asked for fields;
nothing is kept at package level.

The generator reads only the shapes the specification has today, and anything else fails the generation:
a key it does not know, a second schema in `allOf`, a discriminator on another property, a reference to
nothing, a schema that extends itself, one `$type` for two schemas. ADR-0004 measured what a generator that
exits 0 on input it did not understand is worth. None of these refusals fires on today's specification, so
`make ytapi` cannot see one go missing: `scripts/catalogue_test.go`, which `make go` runs, feeds the generator
each shape it refuses.

The directive lives in `internal/youtrack/catalogue.go` and runs under `make generate` with the other one.
`make ytapi` regenerates the catalogue in its copy of the tree and holds it to the assertion
`internal/ytapi` is held to — the file in the tree is byte for byte what its inputs give — so there are
still five assertions. The catalogue names no identifier of `internal/ytapi`: neither ADR-0004's list nor
the seam check of ADR-0006 moves.

## Considered options

- **Absent is refused**, ADR-0001's letter. False refusals on exactly the expressions ADR-0002 and
  ADR-0003 are built on.
- **Absent is left out.** A misspelled name prints a record without its field and exits 0 — the silent
  fallback that killed `youtrack-cli`.
- **A key the named type declares is `denied`**, since rights may hide it. The same absence comes from a server
  that failed to send the field, so the code would guess the class, which ADR-0005 refuses; the message names
  rights as a possible cause instead.
- **The nearest schema the types named at a place of no schema share**, this ADR's first rule. It made the
  outcome a function of the data: two period fields refused `bundle` as `unknown_name`, with no `bundle` among
  the nearest names, while a period and an enum field left it out.
- **The types named there, each with its descendants.** The same false refusal on a project whose fields are
  all of one subtype.
- **The specification embedded by `oapi-codegen`** (`embedded-spec: true`). The binary grows from
  5 513 691 to 10 532 413 bytes before any call, pulling in kin-openapi, `x/text` and jsonschema, and what
  it embeds is pruned to what the generated code references: 111 schemas of 218, with no
  `SingleEnumIssueCustomField`, `SingleUserIssueCustomField`, `SimpleProjectCustomField` or
  `EnumProjectCustomField` — the subtypes the judgment turns on.
- **Reflection over the models of `internal/ytapi`.** The Go types keep no relation between a schema and
  its subtypes, and using `internal/ytapi` outside the adapter file is what ADR-0004's seam forbids.
- **Schemas for the subtypes the server sends and the specification lacks, written into the overlay.** The
  overlay describes operations and the catalogue is generated from the raw specification, so the schema would
  have to be kept in a second place and would stay there after the next `make openapi` brought the real one.
- **The label a record of a link arrives under, read as the name of the field.** It is the phrase translated
  into the language of the instance, which is a second vocabulary of names for the same link type and one no
  caller writes.
- **A hand-written list of the names that matter.** A second copy of the specification, and the first to
  rot.
- **Reading `api/openapi.json` at run time.** One binary ships, and the file does not ship beside it.

## Consequences

ADR-0001's "always emitted" narrows to the type: a field is left out on the objects whose type does not
declare it, so two items of one list may print different keys.

Where the specification names no schema, the answer is the evidence, and it is read a hierarchy at a time.
A name any schema of a hierarchy named there declares is left out rather than refused, however far that
schema stands from the one that arrived: `owner` asked under `customFields` of an object the server named
`Project` is left out, because `Tag`, in the hierarchy of `IssueFolder` with `Project`, declares it. Over the
catalogue that widens the family of 78 schemas, `Project` by nine names and `SavedQuery` by fifteen; on the
polygon's `customFields` it widens nothing, since the subtypes of each project there share no schema nearer
than the root. At the root the command names the family, and `owner` asked of a project stays
`unknown_name`. A `$type` the specification does not have — a newer server's — turns every absence the
family would explain into `upstream_lied`, since nothing shows the name does not apply to it, and so does a
schema the specification has where it does not let it stand.

Names the server sends beyond the specification — `users` of a team, `name` of a user — are printed and
never refused, but they are not offered as nearest names either: the catalogue knows only what the
specification says.

**A `$type` the specification lacks is not only a hazard of a future upgrade.** Two of them arrive from today's
server on today's specification, on 18 of the 66 activities of the polygon, which is what the mark above
exists for. Where a place is judged by the rule that prints it, the catalogue never sees such a type and never
has to explain it; everywhere else the third outcome stands as written, and the refusal it gives is the right
one.

A YouTrack upgrade that changes the specification changes the catalogue in the same diff, and `make ytapi`
fails until the two agree.
