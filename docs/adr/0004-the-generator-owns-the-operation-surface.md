---
status: accepted, partly implemented
---

# The generator owns the operation surface

_The regeneration came first: the specification vendored into `api/` from the dev
instance at the version `dev/.env.example` pins, the overlay with its one `x-go-name`
entry, the stub, the `tool` pin, the `//go:generate` directive in the adapter file, and
`make ytapi`, which runs the five assertions below and is what CI calls. Then came the
passage function and the first two `operationId` entries, `GetProject` and `GetProjects`,
then the third, `GetCurrentUser`, the two of a project's fields, `GetUser`, and `GetIssue`
and `GetCustomFields`, each in the commit that first calls its operation through the
adapter. `GetUser` also brought the third kind of entry below: the `query` parameter of
`GET /users`, which the server answers and the specification does not declare. `GetIssue`
brought the second entry of that kind, `customFields` on `GET /issues/{id}`. The knowledge
base brought its four operations — `GetArticle`, `CreateArticle`, `UpdateArticle`,
`DeleteArticle` — and the third entry of the third kind, `query` on `GET /articles`.
Attachments brought their eight operations and the first correction of a **media type**,
which no overlay entry can carry: the attachment of an article goes out as the multipart
the specification declares for an issue._

`oapi-codegen` was chosen by what the hand-written part would cost rather
than by what the generator could do — `$type` dispatch has to be written by hand
under any of them, because the specification carries `discriminator` without
`oneOf`. Then [ADR-0001](0001-partial-responses-are-trees-not-generated-structs.md)
took the bodies away too: requests and responses are trees, because a generated
struct marshals `nil` to an absent key and therefore cannot clear a field. What
the generator was left holding got named but never measured — "136 paths,
parameters, authentication".

This ADR measures it and cuts. The generator keeps the **operation surface**: the
URL, its parameters, the request it builds. Everything that touches a body is
hand-written. The cut is not a convention anyone has to remember — the typed path
to responses is **absent** from the generated package, because the template that
emits it is stubbed.

## What the generator actually does

Measured on the live specification (YouTrack 2026.1, OpenAPI 3.0.1, 136 paths,
248 operations, 218 schemas, **0 `operationId`s, 0 tags**) with `oapi-codegen`
v2.8.0, not read off its README:

| Probe | Result |
|---|---|
| `client: true, models: false` | exit 0, no diagnostic, **356 undefined**: 210 `<Op>Params`, 87 `<Op>JSONRequestBody`, 59 bare component schemas |
| where those 59 come from | `ClientWithResponses` alone — the raw `Client` references no component schema at all |
| `client: true, models: true` on the untouched spec | exactly one compile error in the whole package: `Type` redeclared in `IssueWorkItem` |
| `include-operation-ids: [GetIssues]` | exit 0, **empty `ClientInterface`** — the filter compares against the raw `operationId`, and there are none |
| an overlay selector matching nothing, `strict: true` (the default) | exit 1, naming the selector that missed |
| an **empty** file passed to `user-templates` | silently ignored: `text/template` does not replace a template body with nothing |
| a file of **only a comment** passed to `user-templates` | silently ignored too: `ClientWithResponses` is back, 342 occurrences, because `text/template` counts space and comments as empty — the stub has to hold an action |
| `nullable-type: true` | 130 of 248 `nullable` properties wrapped; the 118 written as `$ref` with a sibling `nullable` stay `*T` |
| `fields` as a declared parameter | present on 210 of 248 operations; `$top` and `$skip` on 54 each |
| `query` as a declared parameter | present on **3** of 248 — `/issues`, `/tags`, `/workItems` — and on none of the rest, `GET /users` included, which answers it all the same |
| an **object** in `update` against an array of parameters | generation stops while reading its own result: `cannot unmarshal object into field Operation.parameters of type openapi3.Parameters`, and the output is not rewritten at all |
| one parameter declared by **two** entries | exit 2, `error creating operation definitions: duplicate local parameter query/query`, the output untouched |
| the configuration chosen below | generation exit 0, `go build` exit 0, 1.10 MB, `ClientWithResponses` **0 occurrences**, `ClientInterface` **335 signatures** |

Two of those rows are the same fact twice: **the generator is silent when it
fails**. It returned 0 on a package with 356 undefined symbols and 0 on an empty
interface. An exit code is worth here exactly what `except Exception` was worth
to `youtrack-cli`.

The two rows about a parameter entry are the same fact the other way round: where the
overlay writes a parameter, the generator is **loud**. Both ways of getting it wrong —
the object that replaces an array, and the day the specification declares `query`
itself — stop generation with the name of the parameter in the message, before anything
is written. That is what makes the third kind of entry below cheap to keep.

## Where the cut falls

`models: false` is not the cut, because it does not compile. The dependency on
component schemas sits entirely in `ClientWithResponses`, so the cut is made
there instead: `models: true`, `client: true`, and `client-with-responses.tmpl`
replaced through `user-templates` with an action that prints nothing, because an
empty file and a file of only a comment are both ignored. What remains is the raw
`Client`, returning `*http.Response`, and `<Op>Params` structs built out of
primitives — `*string`, `*bool`, `*int32` — with no reference to a schema anywhere.

Bodies go out through the `<Op>WithBody(ctx, …, params, contentType, body io.Reader)`
variant, which every operation carrying a body has. The typed variant survives in
the package, but reaching it means assembling an `ytapi.Issue` by hand, which is
not something done by accident; the ergonomic wrong path, `ClientWithResponses`,
is the one that no longer exists.

Keeping the models is what buys the 210 `Params`. They are not filler: `fields=`
is a declared parameter on 210 of 248 operations, and hand-copying that is the
same act, in the same direction, that killed `youtrack-cli` — asserting something
about the API that the API had already said about itself.

## The specification is patched by one overlay

The plan was a reproducible patch script of four edits. Three of them existed
to make **models** work — `x-go-name: DollarType` so they compile, `nullable-type`
for three-state fields, `x-go-type: json.RawMessage` on six `value` slots that
arrive as scalars. Under ADR-0001 no body is a model, and all three lose their
reason. `nullable-type` in particular would not have paid off anyway: it reaches
130 of 248 `nullable` properties and leaves `author`, `creator` and `type` as
plain pointers, so even switched on it does not restore the ability to clear a
field.

What replaces the script is one OpenAPI Overlay 1.0 file, applied by the
generator itself (`output-options.overlay`, `strict: true`), carrying three kinds
of entry:

- **`IssueWorkItem.allOf[1].properties.type` → `x-go-name`.** Without it the
  package does not compile. The plan blamed `$type` in all 95 schemas that
  declare it; measured, the collision is a single one — `$type` inherited from
  `BaseWorkItem` against `type` in the second `allOf` member, both normalized to
  the Go field `Type`.
- **`operationId` on the operations `ytrack` calls.** Names become readable
  (`GetIssue` rather than `GetIssuesId`, `UpdateIssue` rather than `PostIssuesId`),
  and — the reason it is worth doing — each entry becomes a checked assertion that
  the operation still exists at that path with that method: a path that disappears
  on a YouTrack upgrade fails generation, and the error names the selector that
  missed. An earlier revision of this ADR said that without the entries the loss
  would surface only on a live contract run. Measured by removing
  `/admin/projects/{id}` from the specification, it surfaces before that: generation
  exits 0, but `GetAdminProjectsId` is gone, so a call by that generated name no
  longer compiles in the adapter file, and `ClientInterface` falls from 335 methods
  to 331, under the floor asserted below. The entries fail first, at generation, and
  name the path; they are not the only defence. An entry is written where the generated
  name is wrong or unreadable and nowhere else: `GET /users` the generator already names
  `GetUsers`, and an entry saying so would be a copy of the generator's own rule, kept
  in a file that cannot check it.
- **A parameter the server answers while the specification does not declare it**, on an
  operation `ytrack` calls. `GET /users` searches by `query` — the whole of `user list`
  — and `query` is declared on three operations out of 248, none of them that one. The
  alternative was a `RequestEditorFn` in the adapter appending to `RawQuery`: it would
  build a piece of the operation surface by hand, against the title of this ADR, and it
  would notice nothing on the day the specification catches up. Three conditions hold
  this kind of entry to the same standard as the rest:
  - **only a parameter measured against the instance**, never one read off documentation:
    the specification cannot check it, so the measurement is all there is;
  - **only on an operation `ytrack` calls**, for the same reason the 106 unused paths
    stay in the specification untouched;
  - **only one a contract test pins.** `user list --query ytrack.local` answering `total: 0`
    is the assertion that the server still filters: were the parameter to stop being
    understood, the unfiltered catalogue would come back looking like an answer.

  The `update` of such an entry is an **array**, and that is not a style: for overlay
  1.0.0 an object merges into an array by replacing the whole of it
  (`speakeasy-api/openapi@v1.24.0/overlay/apply.go:277–286`), so an object here would
  carry off `fields`, `$skip` and `$top`; an array is appended (`apply.go:349–351`).

  `GetIssue` brought the second entry of this kind: `GET /issues/{id}` filters the custom
  fields of its answer by name through `customFields`, and the specification declares that
  parameter for the collection `/issues` alone. Measured against the instance, by a member's
  token and an admin's alike:
  `?fields=idReadable,customFields(name)&customFields=State&customFields=Type` comes back
  holding those two fields and no others; `customFields=Stat` comes back `customFields: []`,
  the misspelling dropped and no whole block in its place; `state`, `Состояние` and `ТИП`
  resolve, so the server matches letter case aside and by the localized name too. The
  parameter is written `type: array` with `explode: true`, because the server reads one
  parameter per name rather than a list inside one, and `issue show DEV-1 --fields
  'customFields(State,Type)'` printing exactly those two keys is the contract test that pins
  it.

  That entry also carries the one rule of this ADR that is about *when* a parameter is sent
  rather than how it is declared. The server applies `customFields=` to **every** block of
  custom fields in the answer, not to the one the caller named it under: measured, it cuts
  the blocks of the issues at the far end of a link, of the work items and of the activity
  items the same way. So it goes out only where the issue's own block is the only one the
  request asks for. Where the request holds another — `--fields
  'customFields(State),links(issues(customFields))'` — the parameter is left off, every
  block arrives whole, and the named fields are picked out where they are printed. What the
  output looks like is this tool's decision, and a limit of a parameter of the server is no
  reason to turn an expression the output rules allow into a refusal; all it costs is the
  size of an answer.

  The knowledge base brought the third: `GET /articles` searches it by `query`, which is
  the whole of `article list`, and the three operations the specification declares the
  parameter for are `/issues`, `/tags` and `/workItems`. Measured against the instance:
  `query=project: DEMO` answers the one article of that project, `query=title: Родительская`
  answers `DEV-A-1` and not the child that carries the word nowhere, `query=project: NOPE` is
  `400 invalid_query`, and without `$top` the answer stops at 42 records on an instance that
  holds more. `article list --query "project: DEMO"` answering `total: 1` is the
  contract test that pins it, and beside it one that pins how narrowly it is measured:
  `query=summary: Родительская` — the attribute the same word goes by in the language of issues
  — answers `200` and an empty selection rather than a refusal, so a parameter that stopped
  being understood would look like an answer here as much as an unfiltered catalogue would at
  `/users`. No name is recorded with it: the generator already calls the operation
  `GetArticles`. The `update` is an array for the reason above, and written as an object here it
  never gets as far as carrying `fields`, `$skip` and `$top` off: generation stops on its own
  result — `cannot unmarshal object into field Operation.parameters` — and `ytapi.gen.go` is
  left exactly as it was, so the entry is loud in the way the table above measured rather than
  quietly narrowing the surface.

Work items brought five entries of the first kind and not one of the second. The work items of
an issue hang under a path of five segments, and the generator spells every segment out:
`GetIssuesIdTimeTrackingWorkItems`, `PostIssuesIdTimeTrackingWorkItems`, the same two again
with `IssueWorkItemId` on the end, and
`DeleteIssuesIdTimeTrackingWorkItemsIssueWorkItemId`. The five readable names —
`GetIssueWorkItems`, `CreateIssueWorkItem`, `UpdateIssueWorkItem`, `GetIssueWorkItem`,
`DeleteIssueWorkItem` — cannot be shortened to the obvious ones: `GetWorkItems` and
`GetWorkItemsId` are the names the generator already gives `/workItems` and
`/workItems/{id}`, so the entry does two things at once here, naming the operation and
keeping the name clear of a path that is not the one being called.

The same work measured three things the specification does not know, and patched none of
them. **`Project.plugins`, the schema `ProjectPlugins` and `Issue.timeTracking` are sent by
the server and declared nowhere**, which is what puts the types of work of a project and
the work items of an issue outside everything the catalogue of schemas can judge: a position
under such a key cannot be typed before the request, so the tool's own merging reaches
neither, and the work items of an issue are read by `time list` rather than through `issue
show --fields 'timeTracking(workItems(duration))'`. There is no entry to write for it — an
overlay patches paths and operations, and a missing schema is no selector anything could miss
on — so what pins it is a contract test: `project show DEV` answering `enabled: true` and the
15 types of the project, none of whose names meets any of the 34 its custom fields go by.
**And `POST /api/workItems/{id}` writes**, where the specification declares that path for
`GET` alone; `DELETE` there is `405`. It is measured and it is not called: a work item is
addressed as the child of the issue it hangs from, where all four verbs live and where the
server checks the pair itself, so calling the top-level path would mean writing an entry for
an operation that buys nothing. The measurement is recorded here because the plan for this work
stated the opposite — that there are no top-level paths for a work item — and a
sentence contradicted by the instance is worth keeping where the surface is decided.

Nothing else is patched, and in particular the 106 paths `ytrack` never calls are
not cut out. A list of our own paths, kept in the overlay, is a second copy of
what the code already says, and it would rot first — the same reason no register
of corrections is kept in the source (below).

## A media type the specification gets wrong is corrected at the call site

The overlay renames an operation and adds a parameter the server answers; it says nothing
about bodies, and under ADR-0001 no body is a generated type anyway. So the one correction
of that kind measured so far is made where the call is made: the generated `WithBody`
variant takes the content type as an argument, and the adapter hands it another one.

`POST /articles/{id}/attachments` is declared to take a JSON `ArticleAttachment` and to
answer one object of that schema. Measured against the dev instance in 22 forms — with
`$type: ArticleAttachment` and without it, the content as a `data:` URL, as bare base64 and
as text, with `name` and without — every one is
`500 server_error "Can't instantiate abstract entity type 'BaseArticleAttachment'"`. There
is no JSON that works: the type the server tries to build is abstract and no body names a
concrete one. The other route the specification offers, `POST /api/articles/{id}` carrying
`attachments: [{…}]`, is worse than a failure — `200` with `$type` and without it, and the
article comes back holding exactly the attachments it held before.

What the instance takes is the `multipart/form-data` declared for the **issue** path, the
one part `files[0]`, and it answers an **array** of `ArticleAttachment` rather than the
single object. So one file goes to either kind of owner the same way, and the answer is read
as a list at both. `thumbnailURL` comes back with it — `null` for a text file, a signed link
to a preview the server generated for a PNG — although `ArticleAttachment` declares no such
property; the judgment of names lets that through, since a key nobody asked for is data
rather than a claim ([ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md)).

This is the kind of claim the rule below is written for: the specification cannot check it,
so the measurement is all there is, and a contract test pins it — `attachment create` against
an article of the dev instance, answering one attachment whose preview link fetches
`200 image/png` with no token at all. The day the server starts taking the JSON it declares,
nothing breaks; the day it stops taking the multipart, that test says so in one run.

## Two packages, one passage

`internal/ytapi` holds the machine output and not one hand-written line. Nothing
attaches to a generated type any more, so the plan's estimate of the hand-written
part — "one more `.go` in the same package" — has lost its subject along with its
reason.

`internal/youtrack` is the hand-written side, a deep module with the HTTP stack — a
`net/http` transport that repeats nothing and the bearer editor, as
[ADR-0006](0006-a-package-boundary-needs-a-second-importer.md) lays it out — unexported
inside it, and **one function through which every call passes**. That function puts
`fields=` on the request, calls `ytapi`, decodes the body into a tree, judges the field names
against the tree that was asked for, catches a `200` carrying the login page
instead of JSON, and maps the error. It cannot be bypassed, because no public path
goes around it. ADR-0001 requires that name check on *every* read rather than
wherever someone remembered it, and the map's open question — how `ytrack` refuses
to return a quietly wrong result — has a place to land that will not reopen any of
this. `youtrack-cli` distributed the same duty across its call sites and carried
three defects that each looked small alone.

## Regeneration is checked by assertions, not by an exit code

The vendored `openapi.json`, the overlay and the generated output all live in git;
the generator is pinned by the `tool` directive in `go.mod` and invoked from
`//go:generate`. The canonical specification is taken from the dev instance
(`dev/`) — the same instance the contract tests run against, because a specification
that describes a different server than the tests measure is the class of divergence
this whole ADR exists to close.

CI regenerates and then asserts, because the exit code proves nothing:

- `go build` on the generated package;
- the output in the tree matches what its inputs give — specification, overlay,
  configuration, stub and pinned generator: the same files, byte for byte, as a
  regeneration from scratch;
- `ClientWithResponses` does not occur in the output, so the stub took effect;
- `ClientInterface` carries at least as many methods as it does today (335), so a
  filter or an overlay cannot quietly empty it;
- outside the one adapter file, no identifier from `internal/ytapi` appears beyond
  `Client`, `NewClient`, `*Params`, `RequestEditorFn` and `HttpRequestDoer`.

`make ytapi` regenerates in a copy of the working tree, uncommitted edits included,
and leaves the tree itself untouched. In CI the tree is the commit; before a commit,
`make generate` followed by `make ytapi` passes.

The diff of the regenerated output is also what makes a YouTrack upgrade readable:
it is the API changelog JetBrains does not publish.

## What is not written down

Every hand-written correction is a claim about reality, and it is pinned by a
contract test against the dev instance. It is **not** additionally marked in the
source. A tag saying "the specification says X, the instance does Y" would be a
second copy of the test that already says it, and of the two copies only one is
executed. The code answers where the seam is — that is what the package boundary
is for; the tests answer what is being claimed about reality. On an upgrade what
gets re-read is the test run plus the diff of the vendored specification, and
nothing else.

The contract tests replay `go-vcr` cassettes by default and run against the live
instance under a build tag, which is also what re-records them. A cassette nobody has
checked against a live instance is not a contract test — it is `httptest` with an
extra layer — so a cassette is written by that run and by nothing else: `make
contract` runs the scenarios against the dev instance under the `contract` tag and
rewrites every cassette they replay.

## Consequences

The generated package is 1.10 MB, of which the 218 component schemas are dead
weight: they exist so that 87 body aliases and 210 parameter structs can compile,
and nothing else uses them. That is the price of the surface, and it is paid in
repository size rather than in anything that runs.

The generated package imports `github.com/oapi-codegen/runtime` v1.7.0 and its
`types` package, which makes that module a dependency of the product, not of the
generator: `go.mod` requires it directly, and whatever imports `internal/ytapi` links
it, with `github.com/google/uuid` and `github.com/apapsch/go-jsonmerge/v2` behind it.
It is not dead weight: the package calls it 649 times to style every path and query
parameter into a request, which is the surface itself. The generator's own modules
are the other kind — the `tool` pin lists them in `go.mod` as indirect requirements,
and no package of the module imports them.

The stub is a coupling to a private detail of the generator — a file named
`client-with-responses.tmpl` inside v2.8.0. If a later version renames it, the
stub stops applying, and it stops applying **silently**, because an unrecognized
`user-templates` key is not an error. That is exactly why the assertion list above
contains "`ClientWithResponses` does not occur" rather than trust.

Compile-time safety on bodies was already spent by ADR-0001. This ADR spends the
rest of it: parameters are still typed, but nothing between the tree and the wire
is. The whole of that weight now rests on the contract tests, which makes
the dev instance (`dev/`) the last thing standing between this design and its own evidence.
