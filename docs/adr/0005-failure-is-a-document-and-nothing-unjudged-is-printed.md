---
status: accepted, implemented
---

# Failure is a document, and nothing unjudged is printed

_The nine codes and the refusal document went on stderr first. Then came the read side on
projects: codes by status, the body-shape check on any status, the judgment of names of
[ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md), the counts of a truncated
selection and a transport that repeats nothing. `issue list` brought the first selection of a
caller's own: it runs the assist on every search, warns about what the server looks for as
text, and takes the count from the server's own counter. `issue create`, `issue update` and
`issue delete` wrote the first issue: they hold their own answer against what went out, refuse
before the request what the server would store as something else, and make `write_uncertain`
and exit code 2 observable for the first time. The knowledge base, where the server is silent
about more than it is anywhere else, made the form of an identifier the thing that picks an
API, read a parent before writing one, and left the assist out of a selection of articles.
`comment create`, `comment update` and `comment delete` wrote the first entity that hangs from
another: they address a comment by an owner and an internal id, hold the form of that id before
it can reach a path, and read a comment an issue keeps after its author took it back rather
than writing into one. `attachment create` sends the first body that is not a tree — the file,
written into the request as it is read — and with it the first refusal about a name the
**server** would rewrite rather than one a caller wrote badly. `tag list`, `tag create`, `tag
delete`, `tag add` and `tag remove` are the first commands whose entity is addressed by a name
of the caller's rather than by an id the server gave, so with them came a name resolved against
the whole catalogue the token is shown, a refusal that lists the candidates where the name
answers to more than one, and the rights of a tag read as three sets the server keeps apart.
`link add` and `link remove` write the first link: they resolve a phrase against the slots of
the issue itself, hold the id of the slot to the end the phrase names before anything leaves,
and read the answer to a write from both of its ends. `issue-history list` is the first
selection whose records are not issues: with it arrived the list of activity categories, held
to the instance a category at a time; a truncation proved by a record rather than by a count,
the history being the one selection nothing counts; and a way to hold what the server does
with a request `ytrack` does not send._

[ADR-0003](0003-all-output-is-one-yaml-document.md) settled what an answer looks
like and [ADR-0004](0004-the-generator-owns-the-operation-surface.md) gave every
call one function to pass through. Neither settled what happens when the answer is
not an answer. The map left two questions open — how errors are named, and how
`ytrack` refuses to return a quietly wrong result — and they are one subject seen
from two sides: both ask what the tool is allowed to claim when it is not sure.

The decision has two halves that fit in two sentences. **A failure is a document,
printed by the same emitter as success, carrying a code from a vocabulary that is
ours.** And: **nothing is printed that has not been judged against the record of the
request; nothing is sent that was not built from a structure the tool kept.** The
second sentence has a preference attached — where a vector can be made unreachable it
is not merely checked — and the rest of this ADR is that preference applied to every
vector measured so far.

## What the server actually does

Measured against running instances (YouTrack 2026.1, project DEV), not read off the
specification:

| Probe | Result |
|---|---|
| error responses declared in the specification | **none**: 248 operations, every one declaring `200` and nothing else |
| the `error` key across observed bodies | three vocabularies at once — HTTP reason phrases (`"Not Found"`, `"Unauthorized"`, `"Bad Request"`), snake codes (`invalid_query`, `bad_request`, `server_error`), and `""` |
| body shapes observed | four: `error`+`error_description`; `error`+`error_description`+`error_developer_message`; the same with `error_field` and an empty `error`; and `invalid_query` carrying **`error_children[]`**, one message per unparsed value |
| unknown **field name** on a write | `500 server_error` — a caller's mistake dressed as an outage |
| `ETag`, `Last-Modified` on a read | absent; `If-Match`, `If-Unmodified-Since`, `429`, `Retry-After` occur **0 times** in the specification |
| `query=нетТакогоПоля: Значение` | `200 []` — an empty selection, not a refusal |
| `query=((((` | `200`, and **the same records as an empty query** — the filter is silently dropped |
| `POST /search/assist` on both | `styleRanges` marks the tokens, in five styles and no more: `field-name`, `field-value`, `operator`, `text` on a stretch the server resolved to nothing, `error` on one it tripped over. The array always arrives, `[]` included, and the offsets count **units of UTF-16** — in `State: Opne 😀 привет`, `text(12,2)` is the emoji and `text(15,6)` is `привет` |
| `((((` and `(State: Новая` through the assist | **no mark at all** — not one `text`, not one `error`. The query the server drops in silence is the query the assist says nothing about |
| a stretch the assist marks `error` | the selection refuses the search itself: `400 invalid_query` with `error_children`, on all 19 measured, `State: Opne`, `-тег`, `}` and `sort by: nonexistent` among them |
| `POST /issuesGetter/count` on a search not counted before | `-1`, and a second question straight after the first answers a number on every fresh search measured, and a pause of 0, 50, 100 or 250 ms changes nothing |
| `fields` left off the counter or the assist | the `default` the specification declares for it is not applied: the counter answers `{"id":"count",…}` and the assist `{"id":"suggestions",…}` |
| `$top` left off a selection on an instance with more than 1000 issues | exactly 1000 records — a limit of the server's that no caller asked for and no answer mentions |
| `styleRanges` in the `SearchSuggestions` schema | **not declared** — the server returns a property the specification does not have |
| `fields=idReadable,customFields(name,value(name)),customFields` | `200`, the sub-selection lost **and `idReadable` gone with it** — one key in two forms takes a sibling down |
| `categories` on `/activities` | mandatory, while the specification marks it optional: without it the answer is `400 {"error":"Bad Request","error_description":"No requested categories specified as a filter parameter"}` |
| `categories=BogusCategory` | `200` and an empty list — an invented filter value is indistinguishable from a quiet issue. So are `linkscategory`, `LINKSCATEGORY`, `Links`, `Links Category` and an empty `categories=`: the server matches a category letter for letter and answers everything it does not know with no history at all |
| the list of categories the server would give | there is none: `/api/activityCategories` answers `404`, `/api/activities/categories` `400`, the specification holds 0 identifiers of one, and the three bundles of the web interface hold none either |
| `$top` left off `/activities`, or `-1` | exactly 1000 records where more are there, the silent cut a selection of issues has — and there is no counter to ask instead: activities are counted nowhere, by no endpoint and by no key of the answer |
| `reverse=true` over the 1000 newest activities of an instance in use | `timestamp` never rises, many of them equal to the one before, and `sort by:` in the query changes nothing |
| `{"id":"PT7H"}` on a period field | `200`, and the field is emptied — a write that reports success and takes a value away |
| free text on the way in | the server stores something else and says nothing: a title keeps no line ending (LF, CR, U+2028 and U+2029 become a space, a CRLF two of them, U+0085 is dropped), a description loses every carriage return, a string field is trimmed at both ends, and `""` is stored as `null` by a string, a text and a description alike |
| a field a condition hides, on a creation | `200`, and the issue is filed without the value — on an **update** the same write is `400 invalid_properties` in the server's own words |
| a field nothing wrote, moved by a workflow | `State` set back to `Новая` in the same body as `Fixed in build: 13757` comes back with the build empty: the workflow `clear-on-unresolve` took it away under a `200`, and the `State` did move |
| `WroteRequest` of `net/http/httptrace` | fires once the body and the final flush are through — the same border `net/http` decides by whether a request may be sent again, through a `nothingWrittenError` it does not export |
| a `502` or a `504` carrying HTML on a write | written by something between `ytrack` and YouTrack, which may well have passed the request on; a `500` carrying YouTrack's own `{"error":…}` is YouTrack itself answering |
| an internal id, a Hub id or `me` where a login is addressed | `200` and a user: `/users/2-1` answers `{"login":"admin",…}`, `/users/me` answers `{"login":"admin","$type":"Me"}` — a different user than the string names, and nothing in the answer says so |
| `fields` on `POST /issues/{id}` | honoured — the write answers with the state after itself, **cascade included**: on an instance whose workflows move other fields with `Статус разработки`, writing it alone came back with `State`, `Статус анализа` and two date fields moved, none of them written |
| an article the token may not see against one nobody wrote | both `404`, in **two different sentences**: `Can't find article with id DEV-A-99999` for the one that is not there, `Entity with id DEV-A-1 not found` for the one rights hide |
| a readable id under the `id` member of a body | `400 Invalid structure of entity id` — `{"id":"DEV-A-1"}` is refused where `{"idReadable":"DEV-A-1"}` is taken, so a write that addresses an entity by the name a caller types has to read its internal id first |
| `parentArticle` naming an article the server has none of | `200` and no word: a creation files the article at the root of the knowledge base, an update **takes the standing parent off** |
| `parentArticle` naming an article of another project, on a creation | `200`, and the article is filed in the project of the parent rather than in the one the body names; the same body on an update is `400` in the server's own words |
| an article moved under its own descendant | `500` carrying `{"servlet":"gap-rest-servlet","message":"Request failed.","url":"/api/articles/<id>","status":"500"}` — no document of YouTrack's, over a knowledge base nothing changed in |
| `$top` and `$skip` on one article — `$top=0&$skip=1`, `$top=abc` | `200`, the same document as without them, nested `childArticles` uncut; the specification declares neither for the operation |
| `DELETE /api/articles/{id}` | `200`, empty body, and the whole subtree with it: children and grandchildren answer `404` afterwards |
| `content` of an article on the way in | kept byte for byte — a CR, a CRLF, a TAB, spaces at the end of a line, U+2028, 131071 bytes of it — where the description of an issue loses the CR; `""`, `null` and no key at all all come back `null`, so an empty text is no text |
| `summary: <word>` in a search of articles | `200 []` — an attribute of the language of issues, answered with an empty selection rather than a refusal, while `title: <word>` of the language of articles filters |
| an empty or traversing id where a child of an entity stands in the path | **another endpoint under a `200`**, not a refusal: `POST /api/articles/{id}/comments/` files a **second comment** and answers with it, and `DELETE /api/issues/{id}/comments/..` leaves as `DELETE /api/issues/{id}/` and **took the issue away**. Both probed live, the second on an issue of its own, which it ate |
| `/api/issueComments/{id}` and `/api/articleComments/{id}` | `404` on `GET`, `POST` and `DELETE` alike, carrying `"HTTP 404 Not Found"`, where a comment its owner has none of carries `Entity with id 7-12 not found`: the **sentence** is the only thing that parts a path the server does not route from an entity it does not hold |
| `{"deleted":true}` on a comment of an issue | `200`, `text: null` and `updated` still `null`; the comment stays in the nested list with `deleted: true` while `issue show` leaves it out, and a write into it afterwards is **taken with a `200`** that answers `text: null`. `DELETE` takes it away for good and a `GET` after that is `404`. An `ArticleComment` declares no such member and the server ignores one |
| the text of a comment on the way in | kept **byte for byte** at both kinds of owner and on both verbs — 32 texts, a lone CR, a CRLF, a NEL, U+2028, a BOM, a NUL, trailing spaces, U+1F600 and 300 000 bytes among them — where a title loses a line ending and a description its CR. `""` is `400 "Comment can't be empty."` at an issue and a stored `""` at an article, and `text: null` empties the text of an article's comment under a `200` |
| the id of a comment in the path | matched exactly: `7-011`, `7-02`, `07-2`, `" 7-2"` and `DEV-33` are all `404` where `7-11` or `7-2` stands, so no leading zero is read away |
| whose id the `404` of a comment names | the server's own text is inconsistent: a comment addressed under an owner that does not hold it names the **comment**, the same call with a token that may not see the owner names the **owner**. Both pass through verbatim |
| the same two `404`s at a work item | the same split, measured on its own rather than carried over: a work item of somebody else's issue names the **work item**, and a token that may not see the issue names the **issue** |
| `DELETE` of a work item | `200`, an empty body and **no `Content-Type` at all**, `fields` ignored; a second `DELETE` of the same work item never gets that far, since the read before it is already `404` — one request, not two |
| `Затраченное время` of an issue after a work item of it is removed | the length of the work item that is left, recomputed by the server; the answer to the `DELETE` says nothing of it, having no body to say it in |
| the name a part of a `multipart/form-data` carries | rewritten after the file is stored and without a word: everything up to the last `\` or `/` cut away, a `"` cut with it since the writer escapes it with a `\`, a CR or an LF kept as the four characters `%0D` or `%0A`, bytes that are no UTF-8 replaced by U+FFFD, and both ends trimmed the way Java's `String.trim` does — **by the code unit**, so U+00A0 and U+3000 survive at either end while a TAB, a VT and a FF are cut. Everything else is kept byte for byte, spaces, semicolons, `%41`, U+1F600, a BOM and an NFD form included. A name the trim empties is `400 invalid_properties "Property IssueAttachment.name is invalid"`, and a part with no `filename` at all is filed as `files[0]` |
| a file larger than the instance allows | `400` naming the limit in the server's own words — `the request was rejected because its size (10485761) exceeds the configured maximum (10485760)` — and three times over the limit answers the same, with no error of the write. Exactly the limit is `200`. The setting itself, `maxUploadFileSize`, is 10 MiB on the polygon and whatever the administrator set on another instance, and a token with no role over it reads `200 {"$type":"GlobalSettings"}` with the key **silently absent**. An empty file is `200` and `size: 0` |
| the 501st file of one owner | `400 invalid_properties`, in Russian and naming 500 as the limit; an article answers the same about articles. `multipart/form-data` carrying no part at all is `200 []` |
| a signed link | `200` and the bytes with no `Authorization` header at all, and `200` for a token whose reader may not see the issue. Without `sign` it is `400 "Sign parameter is required"`, a bearer token included — the token does not stand in for the signature. A signature that does not hold — `abc`, the same text in upper case, one byte of the HMAC changed, the expiry moved — is `301` to `/issue/attachment` with an empty body, a page of the web interface rather than a refusal |
| one file read by two readers | **two different links**: the signature carries the id of the reader who asked, beside the id of the file and an expiry at midnight UTC within three days. Both links open the file for whoever holds them |
| an attachment addressed under an owner that does not hold it | `404` on `GET` and `DELETE` alike, the file untouched — another issue, an article where the issue stands, an issue where the article stands. A token the **owner** is not answered for is `404` naming the owner rather than the attachment, and the id is matched exactly: `012-2` and `12-02` are `404` where `12-2` stands |
| where the `404` of an attachment's owner comes from | the same two sentences the article measured above: `POST /api/articles/DEV-A-99999/attachments` is `Can't find article with id DEV-A-99999` while `POST /api/issues/DEV-99999/attachments` is `Entity with id DEV-99999 not found`. The words are the owner's API, not the attachment's |
| the link of an attachment that is gone | `404 "File with id 12-17 not found"`, where the attachment itself is `Entity with id 12-17 not found` — a different sentence for the file and for the entity. Deleting the owner takes its attachments and their generated previews the same way |
| the `$type` of the owner under an attachment of an article | `Article`, where the specification declares that property as `BaseArticle`: the server names the concrete type, which is the one [ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md) reads a place by |
| `/api/tags` asked by two tokens of one instance | two catalogues sharing not one record: the built-in star is a tag per user and nobody else's business, so the polygon answers its admin with four tags and a token with no role with the one star of its own. A tag becomes common to two tokens only when its owner shares it |
| the name of a tag on the way in | both edges trimmed, by Kotlin's `Char.isWhitespace` and by no category of Unicode's: probed rune by rune at each edge and inside the name, the TAB, the LF, the VT, the FF, the CR, U+001C…U+001F and every `Zs`, `Zl` and `Zp` are cut away, while U+0001, the NEL, the zero width space and the BOM stand where they were put. Inside the name everything is kept, a LF and a double space included. A `,` or an `&` anywhere in it is `400` naming the characters in the server's own words, HTML escapes and all — `&quot;&amp;&quot;, &quot;,&quot;` — and a name another tag of the same owner carries, in any letter case, is `400 invalid_properties` |
| `{"name":…}` or `{}` where a tag is hung on an issue or an article | `400 Чтобы найти сущность типа Tag, укажите ее ID` — the member takes the internal id and nothing else, so no tagging makes a tag along the way. Hanging one twice is `200` and no duplicate |
| what the right to hang a tag hangs on | `tagSharingSettings` and no other member: a tag a token is merely shown is `403` on `POST /issues/{id}/tags`, the administrator's own tag included, and so is one it may rename through `updateSharingSettings`; `tagSharingSettings` naming a group the token stands in is `200`. Being shown a tag and being able to hang it are two different rights, and only the second has a member of its own |
| the words of that `403` | **two at once, in two languages**: `error_description` is `Не удалось отметить задачу тегом` and `error_developer_message` is `Can't tag issue`. The locale of the instance picks neither — both arrive in the one body |
| `/api/groups` asked by a token with a global role but no administrator's | `403 {"error":"Forbidden","error_description":"HTTP 403 Forbidden"}`: `jetbrains.jetpass.group-read` belongs to `system-admin` alone, and `/api/admin/groups` is `404` on both instances. The one listing of groups there is is therefore closed to an ordinary member |
| `DELETE /api/{issues,articles}/{id}/tags/{tagId}` | `200` with an empty body, and **the tag stands**: `GET /api/tags` still lists it and the same tag hangs again. Taking a tag off an owner and destroying it are two operations, and only `DELETE /api/tags/{id}` is the second |
| an id of a link slot with no suffix | read as the **target** end and nothing said: `GET …/links/163-1` answers the slot `163-1t`, a `POST` into `163-1` writes `depends on`, a `DELETE` on it takes that link away, while `163-0s` answers `163-0`. On every slot measured, a slot an issue stands at either end of is addressed by digits, one it stands at the source of ends in `s` and one at the target in `t`, without exception — and the specification declares `issueLinkId` a bare `string` |
| a link of an issue to itself | `200`, the issue itself in the answer, and no link written — on Depend and on Relates alike |
| a link written twice | `200`, and no second link: the server writes the one that is there |
| a link taken away | the other end goes with it on all nine phrases, and what a workflow did stays: an issue the `Duplicates` workflow moved is still `Duplicate` afterwards |

Two of these rows are the same fact as the specification's silence: the server does
not report what it did not understand. It drops it, and answers `200`.

## The failure is a document

Everything `ytrack` writes to stderr is written by the renderer of ADR-0003 — prose
in literal blocks, strings quoted, time in ISO-8601 — and a refusal is a mapping
whose first key is `code`. ADR-0001 already fixed what a refusal carries: the code,
the field that did not arrive, the nearest valid names for that schema, and the
`fields=` expression actually sent. That is structure, and structure printed by
`fmt.Errorf` at three call sites is structure with three shapes.

The `request` key of that document is **the request that went out**, not a reading of
it: sent again it reaches the same endpoint and asks the same question. Only the `,`,
`(`, `)` and `$` a `fields=` expression is written with are unwrapped — a query
carries all four literally and no parser of one reads them as a delimiter — while the
identifier in the path and the text of a search stand escaped, the way the server
received them. Unescaping the whole address reads better and names another request: a
slash in a login becomes a second path segment, `#` cuts the address short, `&` cuts a
query in two, and a space and a `+` become the same character.

**An identifier of the instance that went out stays in that key.** The address of a write of
a link carries two — the id the server addresses the slot by and the internal id of the
partner — and neither means anything on another instance. Putting the phrase back in their
place would read better and would be a claim about a request nobody sent, so they stand in
`request` and in `upstream_*` word for word. Beside them, in every refusal of `link add` and
`link remove`, stand `issue`, `phrase` and `partner`: the readable ids and the phrase the
caller typed, which is what the caller sends again. No key that names something carries an
identifier of this instance, and stdout carries none at all — the phrase stands where the id
of a slot would.

stderr is therefore a **YAML stream**: zero or more warnings, then at most one
refusal, separated by `---`, and `yq ea` reads the whole of it. The delimiter that
ADR-0003 threw out of stdout is safe here for the reason it was unsafe there: every
piece of foreign prose arrives inside a literal block, indented, and the separator
stands in column zero. Warnings draw on the same vocabulary as refusals, because two
code spaces on one stream is two things for the reader to keep straight and one of
them will be guessed.

stdout stays exactly as ADR-0001 left it: on any failure it is empty, and the exit
code is non-zero. A partial record on stdout is processed silently by whatever comes
next in the pipe.

## The vocabulary is ours

Mirroring the server's `error` was the cheap option and it is refused. That key is
not a vocabulary: it holds reason phrases, snake codes and the empty string, and its
class is sometimes a lie — an unknown field name is a caller's typo reported as
`500 server_error`, which every retry policy reads as an outage. There is also
nothing to mirror formally: the specification declares no error responses at all, so
no generator produces typed errors and the mapping is hand-written by construction.

A code names **what the caller does next**, not what broke inside. Nine of them:

| Code | Raised when | What it tells the caller |
|---|---|---|
| `bad_usage` | the call did not assemble: identifier form, a native name inside `--field`, wrong arity, mutually exclusive flags; or the address or the token cannot be sent as given; or the metadata of the project shows the call cannot be sent as written | fix the call, or the variable the message names; nothing was written |
| `unknown_name` | a name did not resolve locally: field, link phrase, activity category, the tag or the group a caller named | fix the name; the document carries the nearest valid ones, or the candidates where the name answers to more than one |
| `missing_required` | mandatory project fields are absent on create, or an update empties them | all of them are named at once, not one per round trip |
| `not_found` | the server answered `404` | the identifier |
| `denied` | `401` or `403`; before any request, no address or no token to send, or a file of login records the system will not let `ytrack` read, write or take away | the address, the token or the rights |
| `rejected` | the server refused a write for a reason of its own (`400`) | read `upstream_*` |
| `upstream_failed` | `5xx`, whatever its body; a transport failure, a timeout | the server is down; running it again may work |
| `upstream_lied` | the answer does not match the request: not JSON, a key that was asked for and did not arrive, a write that reports success and changed nothing | running it again will not help; this is the class the second half of this ADR exists to catch |
| `write_uncertain` | the request left, the answer did not arrive | it is unknown whether it happened; deciding that is the caller's |

The last two are split rather than merged because the next action differs and nothing
else in the output distinguishes them. The server's own text passes through verbatim
under `upstream_status`, `upstream_error` and `upstream_message`, and is never
rewritten: the broken template `Сущность типа НетТакогоЗначения с указанным именем
({1}) не найдена` is evidence, and tidying it away destroys the only trace. Anything
unmapped arrives under `upstream_failed` with the raw body attached — never under the
nearest plausible code, because guessing the class is the silent fallback that killed
`youtrack-cli`.

The codes are a contract: documented, added to, never renamed.

**One code carries three shapes where a name is an identity only in a pair**, and each
names a different thing to write next. The tag is the first entity of `ytrack` addressed
by a name of the caller's, and it is unique per owner rather than per instance, so
`unknown_name` arrives as `unknown: [{tag, nearest}]` where the name answers to nothing,
as `ambiguous: [{tag, candidates: [{name, owner}]}]` where it answers to more than one,
and as `unknown: [{tag, owned_by, candidates}]` where the name did resolve and the
`--owned-by` beside it fitted none of the owners it resolved to. The third shape carries
no `nearest`: the name is right, and the nearest names to it would answer a question
nobody asked. All three are raised against the catalogue the token was just shown, so the
`request` key names that one read and nothing was written.

**`bad_usage` says the call is the caller's to fix, not that nothing was read.** A write
reads the metadata of its project before it can tell a value it cannot send from one it
can: a one-valued field named twice, a period written as `P1D`, a date at a time of day,
a field emptied and filled in one call, a field the body's own values leave hidden. None
of these is visible until the project has said what the field is, and all of them are
the caller's to mend — so they are `bad_usage`, raised after one `GET` and before
anything is written, and the document carries the request that went out, the project and
every value refused, all at once. The promise the code keeps is about what the caller
does next and about the instance being unchanged, which a read leaves true.

The same holds where a write names a parent. Which project an article hangs in and what stands
above it in the tree are the server's to say, so the read before the write is what turns
`--parent DEMO-A-1` on an article of `DEV` and a move under an article's own descendant into
refusals the caller can act on — `bad_usage` after one or two `GET`s, with the request that went
out, the article, the parent and, where the line closes into a ring, the chain that closes it.

## Two non-zero exit codes

The exit code carries **only what changes the caller's next action without reading
anything**. `1` — it did not happen, the state is what it was. `2` — it left, and
whether it happened is unknown. That is the whole distinction that cannot wait for
stderr to be captured and parsed.

**The second meaning is a fact about the request, not a property of the code.** A write
answered `2xx` has happened, however the refusal that follows reads: the comparison
finding a value came back rewritten is `upstream_lied` over an issue that by then
exists, and `1` there would tell a caller the state is what it was — which on a creation
invites filing the issue a second time. So a `Fault` carries whether the answer to a
write arrived `2xx`, and exits `2` on that fact as readily as on `write_uncertain`. The
measured case is the sharpest one: a body setting `State` back to `Новая` with
`Fixed in build` in it comes back with the build empty, taken away by a workflow — the
refusal is `upstream_lied`, stdout is empty, and the `State` did move.

Everything else — which field, which nearest names, which `fields=` went out — lives
in the document and is not restated as a number. A code per class would be a second
vocabulary for what the first already says, which is the mechanism that got
`/api/commands` rejected in ADR-0002 and `search` rejected in the map.

## Nothing is retried

No request is repeated: not writes, not reads. A transport failure, a timeout and a
`5xx` are all `upstream_failed`, and the caller — an agent that already composes with
`&&` — decides whether to run the command again. `go-retryablehttp`, named for the
first generated client and in ADR-0004's transport, **leaves the stack**. Nor does `net/http` repeat
a request by itself: it would, on a reused connection that breaks before the answer, on
a redirect, and over HTTP/2 on a GOAWAY or a refused stream, and the transport of
ADR-0006 allows none of them. Pacing is not repetition, and this ADR does not refuse it;
ADR-0006 says when a rate limiter arrives.

The reason is not economy. A retry policy is a set of guesses about which failures
are transient, and every wrong guess is either a duplicated write or a loop against
an error that will never pass — the `500` on an unknown field name is exactly that
loop, and it is only one of the cases we happen to have measured. Failing at once is
the behaviour that needs no such table.

**Asking the counter a second time is not a retry**, and the difference is exactly the
one the paragraph above draws. `POST /issuesGetter/count` answers `-1` while it is
still counting, and `-1` arrives under a `200` whose shape has already been checked: it
is a value of the counter's own protocol, saying the count has started and has no
number yet. A retry repeats a request that *failed*, on a guess that the failure was
transient; this repeats a request that succeeded, on what its answer said to do. Three
boundaries keep the distinction honest, and they are what the code is built around:

- **only `-1` is repeated.** A transport failure, a `4xx`, a `5xx`, a body that is not
  JSON and a body with no `count` in it end the command at once, on either of the two
  calls.
- **exactly once, with no pause.** Every fresh search measured answered with a number
  on the second question, and on the dev instance (`dev/`) the number
  arrives four milliseconds later; a pause would be a pace nobody measured, and a loop
  until ready would be the unbounded wait retries are banned for. The count of repeats
  depends on neither a clock nor a code.
- **the call writes nothing**, so no duplicated write can follow from it.

A second `-1` is a total that is not known, and the document says so rather than
guessing at it: `total: null` and `truncated: null` (ADR-0003).

**Write uncertainty is handed out as it is.** When the answer to a write does not
arrive, `ytrack` does not read back to find out what happened: it reports
`write_uncertain`, names the request that went out, and exits `2`. Reading back would
resolve the case where identity is known and would leave creates undecided anyway,
and the caller has `issue show` and `issue-history list` for exactly this.

**The border is whether the request left whole, and it is read off `httptrace`.** A
transport failure says which call broke, never how much of the request the server
already holds, so the question is answered where `net/http` answers it for itself:
`WroteRequest` fires once the body and the final flush are through, which is the same
border the transport decides by whether a request may be sent again — through a
`nothingWrittenError` it does not export, leaving `httptrace.ClientTrace` the only way
to see it from outside. Before it the request is one no server acts on, its
`Content-Length` not adding up, so the failure is `upstream_failed` and the exit code is
`1`; after it nobody can say, so it is `write_uncertain` and `2`. An answer that begins
and breaks off — a status arriving and the body being cut short — is the same
uncertainty under a 2xx or a 5xx, while under any other status the server has already
said it refused the write, and a body that breaks off does not unsay that.

**Which calls this border applies to is declared, never inferred from the method.**
`issue list` asks two of its questions with a `POST` and changes nothing, so a lost
answer there is `upstream_failed` and exit code `1`. A write goes out through a passage
of its own, and the name at the call site is what says so.

**A `5xx` on a write is split by whose words the body carries.** The table of codes
above reads every `5xx` as `upstream_failed` — the server failed, the state is what it
was — and that holds only where YouTrack is the one answering. A `502` or a `504`
carrying HTML was written by something between `ytrack` and YouTrack, which may well
have passed the request on and lost only the answer, so it is `write_uncertain` with
the status and the body attached. A `5xx` whose body is YouTrack's own `{"error":…}` stays
`upstream_failed`. This split is a correction to the table above and belongs to writes
alone: on a read there is nothing to be uncertain about.

**A write still checks its own answer**, and this is not a contradiction: `fields` is
a declared parameter on `POST`, so the state after the write arrives in the same
response — the cascade of ADR-0001 included, which is what makes the answer worth
returning to the caller rather than merely comparing. The check costs zero requests,
and it is the only thing that catches `{"id":"PT7H"}` — `200`, and nothing happened.
Fail fast means not heroically repairing failures, not declining to look at one's own
answer.

## Nothing unjudged is printed

The rule, in two sentences:

> Everything `ytrack` sends it builds from a structure it keeps; the only
> server-side language it composes is `fields=`, and it composes it from the parsed
> tree, never from concatenated text.
>
> Nothing reaches stdout that has not been judged against that structure; what
> cannot be judged is not claimed.

And the preference that orders the work: **where a vector can be made unreachable, it
is not merely checked.** A check is a thing to remember at a call site, and
`youtrack-cli` died of three defects sitting at three call sites, each small on its
own. ADR-0004 already put every call through one function, so the checks that remain
have exactly one home.

Each vector measured so far, and which half closes it:

| Vector | Closed by |
|---|---|
| one key in `fields=` in two forms, taking a sibling key with it | **unreachable** — `fields=` is emitted by a canonical walk of a deduplicated tree, so two forms of one key cannot be assembled |
| a value pasted into a query, splitting into a field name and a free term (upstream `#721`) | **unreachable** — values go into JSON structure; `ytrack` composes no query text |
| an unknown field name dropped silently under `200` | check — the requested tree is walked against the response, stopping where the parent came back `null` or `[]` (ADR-0001), and an absent key is judged by the `$type` the server named ([ADR-0007](0007-a-name-is-judged-by-the-type-the-server-named.md)): a field the named type lacks while a sibling declares it is left out, and a name no schema of its place declares is `unknown_name` |
| `200` carrying a login page instead of JSON | check — at the entrance of the passage function, by the shape of the body, on **any** status rather than on `200` |
| a write reporting success and changing nothing | check — the write's own answer, compared by identity on the fields written (ADR-0002) |
| a value the server would store as something other than what was written | **unreachable** — every rewriting that has been measured is refused before the request: an empty value of any type, a title holding a line ending or a NEL, a description holding a carriage return, a string with a space at either end, bytes that are no UTF-8, a period in days or weeks, a date carrying a time of day. Sending them and comparing afterwards would report `upstream_lied` over an issue that by then holds the rewritten value, and normalising them quietly would be `ytrack` deciding what the caller meant |
| a field a condition hides, dropped from a creation under `200` | **unreachable** — the condition is evaluated against the body about to go out, and a field the body's own values leave hidden is `bad_usage` before the request, naming the field, the field that hides it and the values that would show it. On an update nothing is evaluated: the server refuses in its own words there |
| a write a workflow rewrote after taking it | check — the same comparison, and the reason its exit code is `2`: the write went through, so the refusal is about an instance that changed. Measured: `State` and `Fixed in build` written together come back with the build emptied by a workflow and the `State` moved |
| an invented activity category answered with `200` and an empty list | **unreachable** — a category resolves against the tool's own list before any request, in any letter case, and goes out as the list keeps it; a name that resolves to nothing is `unknown_name` with the nearest names, and nothing reaches the network. The list is hand-written because neither the server nor the specification has one, so it is not a copy of anything: a category stands in it only once the polygon carries an activity of that category, and a scenario of its own holds each row to the instance. What the instance has beside them — the votes of an issue, the reactions to a comment — is `unknown_name` too, which is the price of the list being an assertion rather than a guess |
| a parameter the server quietly stopped honouring, handing back the oldest records first | check — `reverse=true` goes out on every history and the answer is held to it: `timestamp` is merged into the expression and judged not to rise, equality being lawful. Nothing else in a page of activities says which end of the history it came from, so the parameter failing would read as an answer |
| a selection silently truncated | closed: `total`, `returned` and `truncated` are keys of the document (ADR-0003). When as many records arrive as the limit asked for, a second request counts the selection — the first request's own question over ids alone with `$top=-1`, because one helper asks both passes and a count of anything wider than the selection has nowhere to be written; a count below what arrived is `upstream_failed`, the selection having changed between the two, and more records than the limit asked for is `upstream_lied`. Where the server counts a selection itself the counter is asked instead of the second page: on a large instance a selection over ids alone is far heavier and slower than the counter's 44 bytes, and the gap grows with the selection. A count the counter would not give is `null`, and `truncated` with it. **A history is cut off in the one way that is left**: nothing counts activities, and `$top=-1` on them comes back silently cut to a thousand, so a second pass would answer `total: 1000` over an issue holding a hundred thousand. The request asks for one record past the limit instead — judged with the rest, since it came from the same answer, and thrown away afterwards — and that record proves the page was cut without saying how many there are: `truncated: true` beside `total: null`. More records than the limit and the one asked past it is `upstream_lied` |
| an identifier the server resolves by a key other than the one addressed — `2-1`, a Hub id, `me` for a login | **unreachable** — a user is addressed by login and by nothing else, so each form the server answers for with another user is refused as `bad_usage` before the request. Answering it would be the worst shape a wrong result takes: a whole user, printed as the one asked for, exit code 0 |
| an identifier of one entity sent to the API of the other, and an internal id sent to either | **unreachable** — the form of the string picks the API once, before the first request, and a form that fits neither is `bad_usage` naming what the string is. Both APIs answer an internal id: `/articles/177-1` and `/issues/3-19` each resolve to an entity, and the class differs by instance (`177-` on the polygon, another prefix elsewhere), so an internal id is an address that travels nowhere. The other way round the wrongness is quieter: `/issues/DEV-A-1` and `/articles/DEV-1` are plain `404`s, and a `not_found` there would tell the caller the entity is missing instead of that they addressed the wrong kind of thing |
| a parent the server has none of, taken as no parent at all | **unreachable** — the parent is read before the write, and a `404` there is `not_found` with nothing sent. Left to the answer, a creation would come back a root article and an update would come back with the parent that stood there gone, both under a `200`, and the check would report an article that by then exists |
| a parent of another project, taken as a move of the article | **unreachable** — the same read brings the project of the parent, and a project other than the one the call names is `bad_usage` naming both. On a creation the server would file the article in the project of the parent, which renumbers it: the id the caller would be printed is not the one they asked for |
| a move that would close the line of parents into a ring | **unreachable** — the line above the parent is read with it, ten steps to a request, and an article standing in its own line is `bad_usage` with the chain in the document. The server answers such a move with a `500` of a servlet rather than a document of its own, which by the split above is `write_uncertain` and exit code 2 — uncertainty reported over a knowledge base nothing touched |
| an answer that describes a tree the knowledge base cannot hold | check — the line read off the server is held to being a line: an id standing twice in it is `upstream_lied`, and so is a step that neither reaches the root nor adds an ancestor, since reading on from there would send the request that was just sent. What ends the reading is an ancestor that arrived, never the shape of the expression that asked for it |
| an empty text written into an article | **unreachable** — the server keeps `""` as `null` in `content`, so `--content ""` is `bad_usage` before the write, naming the flag to leave out on a creation and `--clear content` on an update. Everything else measured survives byte for byte, the carriage return the description of an issue loses included |
| an id of a child that addresses another endpoint — empty, `.` or `..` | **unreachable** — the form is held before the request and the segment of the path is built from a type nothing else makes, so a request that is not about the comment the caller named cannot be assembled. Left to the server it is not an error at all: `POST …/comments/` files a second comment and answers `200` with it, which the comparison of the text passes honestly — exit code 0 over a comment nobody asked for — and `DELETE …/comments/..` takes the owner away, measured, the issue gone and `id: ".."` printed |
| a write into a comment an issue keeps after its author took it back | check — the comment is read for `deleted` before the write, and one taken back is `bad_usage` with nothing sent. The server takes such a write with a `200`, changes the text where nothing prints it and answers `text: null`, so leaving it to the comparison would report `upstream_lied` over a text that by then is stored. The race the read cannot close — taken back between it and the write — is what the comparison stays there for |
| the text of a comment emptied by the body rather than by the caller | **unreachable** — the text is a required value of the call and the body is built from it alone, so neither `text: null` nor a body without `text` can be sent; either empties the text of an article's comment under a `200`. `""` is refused before the request at both kinds of owner, since an issue answers `400` for it and an article stores it, and one command that takes an owner may not have an outcome that depends on which kind arrived |
| bytes that are no UTF-8 in the text of a comment or of a work item | **unreachable** — `bad_usage` before the request. `encoding/json` would put U+FFFD in their place and the comparison would then report `upstream_lied` over a comment or a work item that by then holds the rewritten text: the rewriting is ours rather than the server's, which is what makes it a refusal before the write instead of a check after it. A work item keeps an empty text, so that is the one thing refused of its `--text` |
| a day written as a moment, filed under the calendar day of a time zone the caller cannot know | **unreachable** — `--date` takes a calendar day, or the midnight UTC `time list` prints of one, and any other time of day is `bad_usage` before the write. The zone that decides is the profile of whoever's token writes, measured: 15:00 UTC of 1 September is kept as 2 September for a profile in Asia/Vladivostok. What goes out is noon UTC of the day that was named, which is that same day from UTC−12 to UTC+11:59; the two zones past it are said out loud in the long help rather than guessed at |
| a duration written as the ISO string the server prints | **unreachable** — the body is built from minutes and carries one key, so `{"duration":{"id":"PT2H"}}` cannot be assembled. Left to the server it is not the silent loss a period field suffers but an honest `400 Для единицы работы должна быть задана длительность`, on a creation and on an update alike — pinned by a contract test that rewrites the outgoing body in the proxy, since nothing else can send it |
| a work item addressed under an issue that does not hold it | check, and the server's own — `GET`, `POST` and `DELETE` all answer `404 Entity with id … not found` with nothing written, so the pair goes out once and comes back `not_found`. Pinned rather than assumed from the comment: a second issue of the run is filed, the work item of the first is addressed under it, and the work item is read back unchanged. Which id the sentence names differs again — the work item where the issue is wrong, the issue where the token's rights are — and both pass through verbatim |
| a type of work the project does not write against | **unreachable** — the name is resolved against the settings of the issue's own project, read in the request that settles the readable id as well, and a name that answers to no type there is `unknown_name` with the nearest ones and nothing sent. The list a caller would misread is the instance's global catalogue, 17 types against the 15 of DEV; sending one of those is `400 Выбранный тип работы не поддерживается в этом проекте YouTrack`, honest and paid for by a round trip |
| a work item long enough to break the sum the issue carries | **not closed** — 2147483647 minutes is taken with a `200`, and the sum the issue carries is an int32 too: on an issue holding nothing else `Затраченное время` reads `PT35791394H7M`, and the next work item of one minute makes the field `null`, which the block of custom fields prints by leaving the key out altogether. Both writes exit 0. Pinned against the polygon by a contract scenario rather than left to this row. `ytrack` refuses only past `2^31`, where the server would overflow the number and answer that the length is negative. A ceiling of its own would be a policy nobody measured, and reading the sum back to judge it would be another request and a race with everyone else writing on that issue — so the cascade is printed as the server says it, `null` included, and never compared (ADR-0002) |
| bytes that are no UTF-8 in the text of a comment | **unreachable** — `bad_usage` before the request. `encoding/json` would put U+FFFD in their place and the comparison would then report `upstream_lied` over a comment that by then holds the rewritten text: the rewriting is ours rather than the server's, which is what makes it a refusal before the write instead of a check after it |
| a file attached under a name the server rewrites | **unreachable** — each of the five rewrites measured above is `bad_usage` before anything is sent, naming the rule, the character and the way past it, which is to attach a copy made under another name. The rewriting is silent and it happens *after* the file is stored, so a comparison would report `upstream_lied` over an attachment that by then hangs from the issue under a name nobody wrote, and taking it back would be a `DELETE` nobody asked for. A `/` cannot arrive at all: the name is the last element of the path |
| an answer to a creation that names no one attachment | check — one file went out, so one element comes back. `200 []` is measured, and any other count is `upstream_lied` carrying `arrived_count` and the body; the file went out whole, so the exit code is 2 and `attachment list` is what says what the instance now holds |
| a file the server kept under another name or at another size | check — the answer is held against what went out: the name word for word, and the count of bytes the stream itself counted, so a file that changed while it was being read is no error of its own. `mimeType` is held to nothing, the server working it out from the bytes |
| the bytes of a file composed into a JSON body | **unreachable** — a creation builds no body but the multipart, and the `base64Content` the specification declares for it is written nowhere. Left to that path the quiet wrongness is measured: base64 without the `data:` prefix is `200` and files an attachment with `size: 0`, `mimeType: null` and a link that answers `404` — a file that exists and holds nothing |
| the bytes of a file arriving in a document | **unreachable** — `base64Content` is `bad_usage` wherever it stands in an expression, at any depth and under any name above it, before any request. The name is unambiguous without asking the catalogue: the specification declares it on the two schemas of an attachment and nowhere else. `ytrack` has no verb that fetches a file and builds no path under `/api/files`, so the only way to the bytes is the signed link, fetched by the caller with a client of their own |
| an attachment of another owner taken away | **unreachable twice** — the attachment is read under the owner the caller named, and a `404` there is `not_found` before anything is destroyed; the server bounds the `DELETE` by the owner as well. A `DELETE` about a file of somebody else's is a request `ytrack` never makes |
| an address of the instance printed as the bare path the server sent | check — normalisation resolves it against the address `ytrack` was pointed at and judges what arrived, `upstream_lied` for anything that is neither null nor an absolute path (ADR-0003). A path printed as it came would be a link the caller cannot follow and cannot tell from one they can |
| a tag kept under a name other than the one written | **unreachable** — every rune the server is measured to trim is `bad_usage` before anything is sent, naming the rune by its code point and the edge it stands at. Left to the answer, a creation would file a tag under a name nobody typed and the comparison would report `upstream_lied` over a tag that by then exists, while a verb of an existing tag would resolve such a name against a catalogue no tag of it can stand in. Everything the server keeps it keeps byte for byte, a line feed inside the name included, so the refusal is as narrow as the measurement |
| a tag made on the fly by the act of hanging one | **unreachable** — the body of a tagging is the internal id the resolver gave and not one key more, and `{"name":…}` is refused by the server in any case. No verb of `ytrack` creates a tag as a side effect of another, which is what keeps a typo from filing a tag instead of finding one |
| a tag a token may hang but is not shown | **unreachable** — a name resolves against the catalogue of tags the token is shown and against nothing else, so a tag standing outside it is one no name of a caller reaches. The server allows the shape: `tagSharingSettings` without the right to see is `200` on the tagging and the issue then displays no tag, an outcome nothing in the answer would confess to |
| a name two owners of it carry, taken as one of them | **unreachable** — a name that answers to more than one tag ends the command with `unknown_name` carrying every candidate and the login of its owner, and `--owned-by` narrows it in one further turn rather than in a guess. Picking one of them would destroy, hang or take off somebody else's tag under a nought exit code; the shape is reached the moment an owner shares a tag whose name is already in the list |
| a tag renamed or reshared by a token without the right to | out of reach — `POST /api/tags/{id}` there is `200` and nothing changes, the class of "`200`, and nothing happened" this ADR catches by comparison. `ytrack` sends the request nowhere: it has no verb that changes a standing tag, so the sharing of one is settled where it is made |
| a tag destroyed by a command that was asked to take it off | **unreachable** — the removal is an operation of its own, `DELETE /api/{issues,articles}/{id}/tags/{tagId}`, and no branch of `tag remove` reaches `DELETE /api/tags/{id}`. Held three ways: by construction, by the journal of the requests a removal sends, and live — after it the tag stands in the listing and hangs again |
| an article still showing a tag a deletion took away | not ours to close — the deletion is asynchronous for an article, measured at up to three seconds, and `GET /api/tags/{id}` inside that window is `200` with the old tag or `500 "Tag[…] was removed."`. `tag delete` claims only what it checked, which is the `200` to the deletion of the id the resolver found, and the contract tests assert nothing about how fast an article catches up |
| a link written at the end opposite the one the phrase names | **unreachable**, then checked — a phrase carries the end it names, and the slot it resolves to is read off the issue the write is made on: one `GET` brings the catalogue as the server applies it to that very issue, addresses and all, so no id is composed out of a type and a suffix and `/api/issueLinkTypes` is never called. The write's own answer is then read from both ends, because a link written the other way round comes back `200` as well: the partner holds the issue at the end opposite the phrase, and the issue nested inside the partner holds the partner at the end the phrase names. Either end missing is `upstream_lied` and exit code 2 |
| a slot addressed by an id whose suffix disagrees with its end, turning the link round under a `200` | **unreachable**, plus a guard — nothing is composed, and the id read off the issue is held to the grammar of its own end before the write leaves: digits and a dash and digits where the issue stands at either end, the same and an `s` at the source, the same and a `t` at the target. Anything else, `.` and `..` among them, is `upstream_lied` with nothing sent. The grammar is declared nowhere in the specification, so it is measured, and held on the instance by contract tests that write each of the nine phrases and read the suffix back off the request |
| a link of an issue to itself, answered `200` with nothing written | **unreachable** — both issues are read before the write, and two arguments that reach one issue are `bad_usage`: `DEV-1` and `dev-1` are one issue and only the read says so, while `3-19` is refused by the form of the argument before a single request, an internal id addressing an entity that has no readable id of its own. Sending either would report a link that is not there |
| the issues of a slot arriving cut down | closed by the document — the counts of `link list` are judged by `issuesSize`, the server's own count beside each slot (ADR-0003), so a collection that is ever cut is declared rather than printed as the whole |
| a write that moves a link nobody named | closed by what is printed — the answer to a write of a link carries the links of the issue after it, so a second `subtask of` prints the one parent that is left and an original that becomes a duplicate prints the duplicates the workflow carried over. What the workflow did to the issue the call was made on is not a link and is not printed: `duplicates` leaves that issue in `Duplicate`, which `issue show` says |
| a phrase a caller wrote in a language of two bytes to the letter | closed by the measure — the distance to a phrase is counted in runes, so `завист о`, two letters off the translation `зависит от` and four bytes off it, is answered `depends on`, and a caller writing Cyrillic is helped as readily as one writing the phrase itself |

Everything in the right-hand column that says "check" fails the same way: empty
stdout, `upstream_lied` or `unknown_name` on stderr, non-zero exit. A single bad name
fails the whole command; ADR-0001 gave the reason and it has not changed.

Making a form unreachable is a **list of refusals**, never a grammar of what an
identifier may be. The list is as narrow as its measurement: of the logins on instances
in use none holds a space, none is shaped as an id of either kind and none is `me`, so
no refusal costs a user their own login — while a permissive form would, because logins
hold `@`, Cyrillic letters and upper case, and some of them look like an email
address. This is ADR-0002's rule about the cache seen from the other side: a
thing allowed to refuse is worth exactly the measurement behind it. What the list does
not name — `ME`, `me2`, `2-1x`, a UUID without its dashes, a full name of one word, an
email address — is sent, and the `404` that comes back is `not_found` naming the login
and the way from a name to one.

**Which owner a child hangs from is the server's own check, so `ytrack` makes none of its
own.** A comment addressed under an owner that does not hold it — another issue, an article
where the issue stands, an owner the token may not see — is `404` on `GET`, `POST` and
`DELETE` alike, at both kinds of owner, and the comment is left as it was. So the form of the
owner picks the API once and the pair goes out once: there is no second request to try the
other API with, no list of a comment's siblings to look the id up in, and the `404` is
`not_found` rather than a reason to ask again. Checking it here would mean reading the owner
before every call to learn what the call itself is about to be told. The one thing the server
is not consistent about is which id its sentence names — the comment where the owner is wrong,
the owner where the rights are — and both pass through verbatim, because the text is evidence
of which of the two the caller got wrong.

A work item hangs from an issue and from nothing else, and the server checks that pair the
same way — measured on it rather than carried over from the comment, since a rule that held
for one child of an entity is a claim about the other until somebody sends the request. It
answers `404` on all three verbs with the work item untouched, and its sentence splits the
same way: the work item where the issue is wrong, the issue where the rights are.

**A file is sent as it is read, and the one failure that buys is named here rather than
guarded against.** The multipart is written into the request while the transport reads the
other end of it, so no size of file is `ytrack`'s to refuse: what the instance allows is a
setting only its administrator sees — 10 MiB on the polygon and another number elsewhere —
and a number chosen here would refuse files the instance would have taken. A buffer would
refuse them differently and worse: a file named by mistake ends in an OOM kill, which is no
document and neither exit code. The stream ends in a document either way, the server's own
`400` naming its limit among them. The price is that a read of the file that breaks partway
reaches the caller as the transport failing — `net/http` unwraps the error of the body and
hands it on — so its code is `upstream_failed` and its exit code `1`. The code is right: the
request left unfinished and no server takes a truncated multipart, so nothing was written.
The class is imprecise, and it stays that way rather than growing a branch: nothing reachable
through `Run` produces it.

**The link printed with an attachment is a pass, and the document is read as one.** The
signature carries the id of the file, the id of the reader who asked for it and an expiry at
midnight UTC within three days, and the server checks the signature and nothing else: no
`Authorization` is needed, and a reader who may not see the issue is answered `200` all the
same. So the pass is issued to whoever asked and works for **anyone it reaches** — which is
what makes it printable at all, since the point is a link an agent can hand to a client that
holds no token, and what makes a printed link something to keep the way a token is kept. It
is also why two runs by two readers print two different links for one file, and why neither
is stable past the expiry: a link is evidence of a read, never an identity of the file. The
help of `attachment list` says exactly this much and no more.

**The rights of a tag are three sets the server keeps apart, and a flag writes each one
of them.** Being shown a tag, being able to hang it and being able to change it are
`readSharingSettings`, `tagSharingSettings` and `updateSharingSettings`, and only the
second grants the tagging: a tag shared for reading is `403` on `POST /issues/{id}/tags`
for its own administrator, and so is one shared for changing. So `--visible-for`,
`--taggable-by` and `--updateable-by` write one set each, the specification's `readOnly`
marking notwithstanding (ADR-0001), and the help of `tag create` says which right each
one hands out. `--taggable-by` is what makes the triage of a team expressible: shared for
reading alone, a tag the whole team sees is a tag none of them can hang, and no wording
of the refusal would have mended that. The refusal a caller does meet passes through as
it came, and it comes twice over — `error_description` in Russian and
`error_developer_message` in English in the one body — which is why both stand under
`upstream_*` and neither is chosen for the reader.

**A group is named and never addressed**, and the single listing that turns a name into
an id is `GET /api/groups`, which YouTrack answers only an administrator: a token holding
a global role and a place in a project is `403` there, `/api/admin/groups` existing on
neither instance. A flag naming a group is therefore `denied` for such a caller and
nothing is written — no tag is made shared with fewer groups than were asked for. There
is no second path: the groups readable off the sharing of the tags a token can already
see are the groups of those tags, and sharing a new tag with a set assembled that way
would be guessing which group the caller meant.

## The query belongs to the caller, and the assist only lights it

`issue list --query "<query>"` and `issue-history list --query "<query>"` hand
YouTrack's own language to YouTrack unexamined. This is the one place the rule above
cannot reach, and it stays that way: a local validator would be a second parser with
different rules from the server's, wrong first on the syntax it does not know
(`#Новая`, `-тег`, `has:`), and the tool does not exist to save a caller from a query
they meant to write. The one thing checked is that the value is text at all — bytes
that are no UTF-8 are `bad_usage` before any request, since the server answers them in
a URL with a `500` naming a `400`, a caller's mistake dressed as an outage again, and
the JSON body of the counter would carry them as U+FFFD, so the two requests would ask
different things. Deciding that reads no token of the query, which is why it is not the
validator this paragraph refuses.

But the two failure modes are ugly enough to light up. An unknown field name gives
`200 []` — which reads as "no such issues" — and a malformed query gives the
unfiltered list, which reads as an answer. `POST /search/assist` settles the first
without a parser of our own: the server marks each token, and a token it read as free
text comes back `text` rather than `field-name`.

**It does not settle the second, and that is a measurement correcting this ADR.**
`((((` on both instances and `(State: Новая` on the dev one come back from the assist
with no mark at all — not one `text`, not one `error` — and the selection answers `200`
with the unfiltered list. The query the server drops in silence is precisely the query
it says nothing about, so lighting up a malformed query is unreachable without the
parser this section forbids, and the tool does not claim it. What the assist does
settle it settles completely: of 19 searches it marked `error`, the selection refused
all 19 with `400 invalid_query`, so a broken query the server *does* read comes back as
`rejected` carrying the server's own text under `upstream_*`.

So the assist is called on **every** selection of issues, before the selection goes out, and
its result is a warning on stderr — never a refusal — naming the query that went out and
the parts of it the server looks for as text. The full markup is not printed: it
describes the server's parser, not the caller's mistake.

- **Only the style `text` warns.** `field-name`, `field-value` and `operator` are the
  query the server read; a stretch marked `error` the selection refuses by itself, in
  the server's own words; a style no measurement named is no ground for a guess. `text`
  is held by contract tests against the polygon, so an upgrade that renames it is
  caught by a run rather than by a reader.
- **The code is `unknown_name`,** which names what the caller does next: whoever wrote
  `нетТакогоПоля`, `for: me` on a Russian instance, or `State: In Progress` — where
  `Progress` alone becomes free text — fixes the name. `bad_usage` promises the network
  was never touched, and it was; `rejected` and `upstream_*` are about the server, and
  the server answered correctly. This is a departure from the table above, which raises
  `unknown_name` for a name that did not resolve *locally* and puts the nearest valid
  names in the document. The warning carries no `nearest`: candidates live in
  `suggestions`, which is the full markup this section declines to print. A tenth code
  was rejected instead — the vocabulary is closed at nine, and a warning draws on the
  same one.
- **The document is `code`, `message`, `query`, `free_text`,** one warning for the whole
  selection. The parts are the caller's own words: the server marks a name it does not
  know and the colon after it as two ranges, which are printed as the one word that was
  typed, and the stretches are cut in units of UTF-16, the way the server counts them,
  so a rune outside the basic plane is taken whole or left out. No style, no offset and
  no `styleRanges` reaches the document.
- **A warning, not a refusal.** `Задача в работе`, `"exact phrase"` and `issue id:
  DEV-1 работе` are a legitimate text search, and the server marks them with the same
  `text`. Refusing would make a text search inexpressible and hand the assist a veto it
  has no standing for; the selection itself is judged as strictly as ever, and the exit
  code stays the command's own.
- **The assist failing is the command failing.** Any status it answers, and an answer
  that never arrives, end the call before the selection goes out. "On every selection"
  means a selection is never printed over a query nothing was said about; printing one
  with "the assist was unavailable" attached would make the diagnostic a function of
  the server's availability, which is the soft degradation refused for the counter and
  everywhere else in this ADR.

**The knowledge base is searched in another language, and the assist does not know it**, so
`article list` asks it nothing at all. `GET /api/articles` takes a `query` written in the
language of articles, and the assist marks every query against the language of issues; the two
disagree in both directions, measured on the same strings through both:

| Query | `/api/articles` | The assist |
|---|---|---|
| `title:`, `content:`, `author:`, `article id:` | filters, correctly | `text` — a warning about a field name that is right |
| `summary:`, `State:`, `reporter:`, `Assignee:`, `issue id:`, `commented by:` | `200 []`, silently | a field name — no warning about a name the knowledge base has none of |
| `#Unresolved`, `#DEV` | `400` | `field-value` |
| `((((` | every article | no mark at all |

The one signal that is right is the `error` the selection itself answers `400` to, which
costs no second request to learn. Everything else the markup would say about a search of
articles is a claim about a grammar that is not the one the search was read by, and there is
nothing to check it against — which is what this ADR forbids printing above. So the call is
absent from the command rather than made conditional on what comes back: a diagnostic that
appears with the data is the shape refused for the counter.

The price is that the two quiet failure modes stay quiet here. The long help of `article list`
names the attributes of the language of articles and says outright that an attribute of the
issue language answers an empty selection and a malformed query answers everything; a contract
test pins both ends of it, `title: Родительская` finding the article and `summary: Родительская`
finding `total: 0`.

## Considered and rejected

- **Mirroring YouTrack's `error` as the code.** Free, and the vocabulary is three
  vocabularies with a lie in it. See above.
- **Typed errors out of the generator.** Impossible rather than rejected: the
  specification declares `200` and nothing else on all 248 operations.
- **Retrying idempotent reads only.** The conventional answer, and still a table of
  guesses about which failures are transient. The caller re-runs a read for the cost
  of one command.
- **`draftId` as an idempotency key for creates.** It exists on `POST /issues` and
  `POST /issues/{id}/comments`, and it would make exactly those two writes safely
  repeatable. Refused: partial idempotency is worse than none, because the caller
  learns that creates may be repeated and carries that to `time create`.
- **Resolving write uncertainty by reading back.** Would decide updates and deletes,
  where identity is known, and would leave creates undecided — matching a create by
  summary and a time window is a guess about identity, which is the class this ADR
  is written against.
- **An exit code per error class.** A second vocabulary for what the document says in
  words.
- **Validating the query against `field list` before sending it.** A second name
  resolver, disagreeing with the server's on everything that is not a field name.

## Consequences

`go-retryablehttp` leaves ADR-0004's transport, which is a correction to that ADR and
to the stack of the first generated client, not an omission. What remains inside `internal/youtrack` is a
transport that repeats nothing, the bearer editor, and the one passage function — which
now also owns the body-shape check, the name judgment, the write comparison and the
error mapping.
That function has grown into the place where being wrong is decided, and that is
deliberate: it is the only place that cannot be bypassed.

A command that takes a selection costs between two and five requests, one after the
other. `issue list` asks the assist, then the catalogue of custom fields where the
caller named one of their own, then the selection, then the counter where as many
records arrived as the limit asked for, and the counter once more where it answered
`-1`. Only the assist is unconditional, because a condition there would mean the
diagnostic appears as a function of the data; each of the others is asked for a reason
written in the answer before it. A sequence of that shape is not the series that would
call for a rate limiter, and ADR-0006 says why none arrives.

`issue-history list` costs two or three, and this is a correction to the "two requests" of
the paragraph above rather than an exception to it: the assist, then the link types of the
instance where the selection may print the phrase a link goes by, then the history. There
is no fourth — nothing counts activities — and the third is decided by what was asked
rather than by what came back, so the number of requests a call makes is a property of the
call.

**What the server does with a request `ytrack` does not send is held by editing the request
between the tool and the polygon.** Three of the assertions below are about a `categories`
the tool cannot be made to send — none at all, an invented name, a name in the wrong letter
case — and through `cli.Run` there is no argv that sends one. The scenario puts the edit in
the test's own proxy, before the recorder, so the cassette holds the request the server
actually saw and the assertion reads the same replayed as recorded. Nothing of the
production code moves for it, which is the whole of why this is allowed: a seam opened in
`internal/youtrack` for a test is what ADR-0006 refuses, and a measurement written into
this ADR and nowhere else would leave the assertion to a reader.

A write of an issue costs two requests and never more: the metadata of the project, or the
issue with its project under it, and then the write itself — which answers with the state
after it, so the comparison and the document the caller reads cost nothing further. A
deletion is the same two, the first of them reading the readable id the second is
addressed by. A write of a link costs three, and the third is a read as well: the issue the
write is made on, for the catalogue of phrases as the server applies it to that issue and
for the address of the slot; the issue at the other end, for the internal id the body takes
and to tell a partner that is not there from the three different `400`s the server would
answer a body naming one with; and the write. The phrase is resolved against the first of
them and against nothing else, so the catalogue of link types is never asked for.

A write of an article costs the same two, and a third only where it names a parent: the article
is read, the parent is read with the line above it, and then the write goes out. Filing a new
article under no parent is one request, since nothing of the project is read first; `--clear
parent` reads no parent either, because there is none to resolve, so taking an article to the
root of the knowledge base is a `GET` and a `POST`. The line above a parent is read ten steps to
a request and costs a second one only where the tree is deeper than that.

**A verb of a tag costs one request for the catalogue and one for what it came to do.**
`tag list` is that one read, and a second only where the page fills the limit; `tag create`
is one write, and a read of the groups before it only where a flag named one; `tag delete`
is the catalogue and the deletion; `tag add` and `tag remove` are three — the owner, read
first so that an owner the instance has none of is `not_found` before any catalogue is
asked for, then the catalogue, then the write. `--owned-by` adds nothing to any of them:
it narrows the candidates of the read that already happened, and the login it carries
reaches no request at all. The catalogue is read whole, `$top=-1`, and never through
`query`: that parameter matches a prefix and folds letter case in a way of its own, so a
tag it left out would read as a tag that is not there, and a name resolving to nothing
needs the whole listing to suggest from in any case.

A write of a comment costs **one** request, and two only where the comment of an issue is
changed: the owner is never read, since every way of naming one the server cannot resolve is a
`404` it answers itself with nothing written, and the answer to the write carries the comment
that was kept. The second request, where it happens, is the read of `deleted` above — the one
thing the server would take in silence. A deletion is one request and prints the id it was
given, the server having matched it exactly to answer at all.

Eight assertions about reality are added to the contract suite, each
measured here and each capable of changing under a YouTrack upgrade:

- `fields` on `POST /issues/{id}` returns the state after the write;
- the set of activity category identifiers the instance answers with records;
- an invented category answers `200` with an empty list;
- `categories` is mandatory on the activities endpoint;
- `styleRanges` is present in the assist response while absent from the
  `SearchSuggestions` schema;
- the style of a stretch the server looks for in the text of the issues is `text`;
- a search the assist marks `error` is refused by the selection with `400`;
- `((((` is marked with nothing and filters nothing.

The first writes add five more, measured against the polygon and held there by contract
tests of their own:

- the class the table sends and the class the server names agree on all twenty rows;
- a field a condition hides is dropped from a creation under a `200` and refused on an
  update;
- a project fills a required field that has a default, and names one missing field per
  round trip;
- a multi-valued field is emptied by `[]` and answers `Field value cannot be null` to a
  `null`;
- a workflow rewrites a field the same body wrote, under a `200`.

The history adds four, three of them sent through the edit above:

- every category of the tool's list answers with an activity of that category, one
  scenario per row, so the list grows by measurement and not by reading;
- a selection with no `categories` is refused `400` in the server's own words;
- an invented category and a canonical one in the wrong letter case both answer `200` with
  an empty list;
- `reverse=true` hands back a page whose `timestamp` never rises.

The knowledge base adds six of its own:

- one article answers `$top=0&$skip=1` and `$top=abc` with the document it answers without them;
- an article rights hide and an article nobody wrote are both `404`, in two different sentences;
- deleting an article takes everything written under it;
- `summary:` finds no article while `title:` finds one, and neither is warned about;
- `content` comes back byte for byte, a carriage return included;
- a parent the server has none of leaves the standing parent where it stood, because the read
  before the write means nothing is sent.

The first links add five of their own, each written and read back on issues the test files
in DEV and takes away again:

- each of the nine phrases of the instance writes the link at its own end, and the partner
  reads it back under the phrase of the opposite end and under no other;
- the id addressing the slot of a link carries the suffix of that end, and the segment of
  the request is the id the read before it sent;
- no document of `link list`, `link add` or `link remove` carries an id the server addresses
  a slot by;
- a second link of a type an issue holds one of takes the first away, and the answer to the
  write is where that is seen;
- a subtask of its own subtask is `400 invalid_properties` in the server's own words, HTML
  entities included.

The comments add five more, each of them a sentence of the server's that a run would catch
changing:

- `/api/issueComments/{id}` and `/api/articleComments/{id}` answer `"HTTP 404 Not Found"`, and
  a comment its owner has none of answers `Entity with id … not found`;
- a comment of an issue taken back is kept, and `issue show` prints it no more;
- a comment taken back is refused a write and removed for good by a deletion;
- the text of a comment comes back byte for byte, a carriage return and a line separator
  included;
- the comments of an owner arrive with it, oldest first by `created`, and a write into one
  moves nothing in that order.

The tags add seven, each of them a right or a rewriting that a YouTrack upgrade could move:

- the three sharing sets are written by the body and come back holding what went out,
  although the specification marks all three read-only;
- a tag a token is only shown is `403` on the tagging, and the same tag shared through
  `tagSharingSettings` is `200` — the refusal carrying its Russian and its English text at
  once;
- `/api/groups` is `403` for a token that is not an administrator, so a flag naming a group
  is `denied` there and no tag is made;
- a name carrying a rune of the trimming is refused before the write, while a line feed
  inside one is kept and the tag is found again by the very same argument;
- a name another tag of the owner carries, in any letter case, and a name holding a `,`
  are the server's own `400`, and its words pass on with their HTML escapes;
- a removal leaves the tag standing: it is listed afterwards and hangs again;
- two tokens of one instance are shown two catalogues that share no record, and a tag
  shared with a group reaches the member carrying the three keys of the list's default.

The timeout and the rate limiter's pace are parameters of the implementation, not
decisions of this ADR: they change on one line and pull nothing behind them.
