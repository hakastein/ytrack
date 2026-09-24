---
status: accepted
supersedes: the rule "a selection is one page and no page follows it", written into the request of every list;
  in ADR-0003, `truncated` as `total > returned` for a list read past a skip; the `--limit` later given to
  `link list` against ADR-0003's "There is no `--limit`"
---

# A list takes any page, and only a list that can takes a limit

Every list of the tool took `--limit` and sent it as `$top`, and none sent `$skip`: a selection was one page,
and a caller who wanted what lay past the limit had to narrow the search until it fit. Narrowing is not always
possible — the comments of an issue or the children of an article take no search at all — so a limit with no
second page left records the caller could see were there and could not reach. The feedback that asked for
`offset` on `list_issues` and `search_articles` and for `childOffset` on the tree of articles said the same.

## Decision

**Every list whose request takes `$skip` takes `--skip N`**, from 0 to the largest int32, beside `--limit`:
`issue`, `article`, `comment`, `attachment`, `tag`, `time`, `user`, `project` and `activity`. The flag is
named after the parameter, as `--fields` and `--query` are. `$skip` goes out on the page alone, and only where
it is not 0, so a first page is the request it always was; the pass that counts a list by ids reads the whole
and carries none.

**`total` is the whole of the selection on every page, and `truncated` is whether records stand past the
page**: `total > skip + returned`. A page short of the limit ends the selection, so `skip + returned` is its
total and nothing is counted. An empty page after a skip says nothing of where the end was — the skip may
have passed it — so it is counted where the list counts, and is `total: null` in the journal, which counts
nowhere. A skip past the end is an empty page, not a refusal: that is how a loop over pages ends.

**`article list --parent <id>` lists the children of an article** from `/articles/{id}/childArticles`, with
the same page and the same count by ids. That path takes no search, so `--parent` with `--query` is
`bad_usage` rather than one of them dropped.

**A list that cannot ask for a page takes no `--limit`.** `link list` reads the links nested in the issue,
which take neither `$top` nor `$skip`, and `field list` reads the whole project because `$top` counts the
fields of the array and not the order the project keeps them in. Both cut what had already arrived, so a
limit there hid records no second page could reach. Both print the whole, under the same document of a list,
with `truncated: false` unless the server says a slot holds more than it sent.

## Consequences

- A caller walks any list with `--limit L --skip 0`, `--skip L`, … until `truncated` is `false`.
- Records added or removed between two pages shift what the next page holds; YouTrack keeps no cursor for these
  collections, and the tool does not pretend to one.
- A count by ids below `skip + returned` is the collection changing between the page and the count, and is
  `upstream_failed`, as a count below the page was before.
