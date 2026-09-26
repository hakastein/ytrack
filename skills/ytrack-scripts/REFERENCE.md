# ytrack command scripts: reference

The signatures are in [v1.d.ts](v1.d.ts) for `ytrack/v1` and in [node.d.ts](node.d.ts) for `fs`, `Buffer` and
`fetch`, in place of `@types/node`; this file holds the rules they cannot say.

## Roots and names

- Roots: the nearest `.ytrack/scripts` above the working directory, then `~/.ytrack/scripts`. Symlinks resolve, so a
  plugin directory links in as `~/.ytrack/scripts/<name>`.
- The file path under the root is the command path: `docs/map.js` → `ytrack docs map`. A directory is a group of
  commands. `x.js` beside a directory `x/` is a conflict: neither becomes a command.
- A first word belongs to one root: built-in, else the project, else the user. Words are lower-case letters, digits,
  `-` and `_`, not starting with `-` or `_`; a path is under eight words. Files and directories starting with `.` are
  skipped.
- A module with no `exports.definition` is a library module. `require` takes `"./…"` or `"../…"` inside the same root,
  `"ytrack/v<N>"`, `"fs"`, `"buffer"` (or `"node:fs"`, `"node:buffer"`) — nothing else. A script never requires a
  command script.

## `exports.definition`

| key | |
|---|---|
| `short` | required; one present-tense line, capitalized, no full stop, ≤ 60 characters |
| `long` | required; written apart from `short`, says only what changes the call |
| `args` | `[{ name, type, usage }]`, `type` is `string`, `path` (completes as a file name) or `duration` |
| `flags` | `[{ name, type, usage, multiple?, choices?, default? }]` |

A backticked word in `usage` names the value in `--help`. An unknown key anywhere makes the definition unreadable.

| flag `type` | `command` gets |
|---|---|
| `string` | the string; with `choices`, one of them |
| `int` | a 32-bit whole number |
| `bool` | `true` or `false` |
| `fields` | the fields expression: `default` when not given or empty, `+expr` is `default,expr`; `default` required, no `usage` |
| `pair` | `Name=value` as `{ Name: "value" }`; a name repeated gathers an array |
| `duration` | ISO 8601 hours and minutes as whole minutes: `PT1H30M` → `90` |

`multiple: true` (only `string` and `pair`) repeats the flag; a `string` then gives an array. A flag with a `default`
is always in the flags object; one without is there only when given. An unknown flag, a value out of `choices` and an
extra argument are refused with `bad_usage` before the script runs.

## Functions of `ytrack/v1`

- One object of named keys, camelCase. A key left out or `undefined` is not given; `null` empties the part (a
  description, a custom field, a work item attribute).
- `fields` is the whole response expression; a function has no default fields. `limit` and `skip` page a list;
  `limit` left out is the SDK's default page, below 1 is `bad_usage`.
- `customFields`: `{ Name: value | [values] | null }`. Work time is whole `minutes`. `comments` is `"all"` or a count.
- `attachments.create` takes either `path` (a local file) or `content` — a string in UTF-8, a `Buffer`, a
  `Uint8Array` or an `ArrayBuffer` — with `name`, which goes only with `content`.
- An extra key, a key of the wrong type, a missing required key, `path` and `content` together: `script_failed` at the
  line of the call.
- The login is looked up at the first function call or read of `address`, so a script that never reaches YouTrack
  needs no login. `address` is the instance URL with a password masked; the token is never visible.

## Answers

- A function returns the document the matching command prints: maps and lists are read-only objects and arrays in
  the order of the answer (strict mode, so writing throws). A key not requested reads `undefined`, one held empty
  `null`. A number a double cannot hold exactly is a `BigInt`.
- A value of an answer prints as its node wherever it goes in the returned object. A value the script builds prints
  by type: a string with a newline as block text, a plain object as a mapping in key order (keys with `undefined`
  dropped). A function, symbol, `NaN`, `Date` or bytes in the returned value is `script_failed`.

## Faults and exit codes

- The codes: `bad_usage`, `unknown_name`, `missing_required`, `not_found`, `denied`, `rejected`, `upstream_failed`,
  `upstream_invalid`, `write_uncertain`; `script_failed` is ytrack's own and neither `fail` nor `warn` takes it.
- A thrown fault is an `Error` with `code`, `message`, `details` and `wrote`. Uncaught, it prints as it is.
- `fail(code, message, details?)` throws; `warn(code, message, details?)` prints to stderr and returns. `details` is a
  plain object without `code` or `message`.
- Exit `0` success, `1` nothing changed, `2` the instance may have changed: `write_uncertain`, or any fault after a
  function that writes (`create`, `update`, `delete`, `add`, `remove`, `writeFields`) succeeded. `ytrack` counts that
  itself.
- An SDK warning (such as free text in a search) always reaches stderr.

## `fs`

`const fs = require("fs")` — the synchronous functions of Node, with Node's arguments and errors:

- `readFileSync(path[, encoding | { encoding }])` → a `Buffer`, or a string in the encoding named;
- `writeFileSync(path, data[, encoding | { encoding }])`, `data` a string, `Buffer` or `Uint8Array`;
- `mkdirSync(path[, { recursive }])` → with `recursive`, the first directory made or `undefined`;
- `existsSync(path)`, `readdirSync(path[, "utf8" | { encoding: "utf8" }])` (names, sorted);
- `statSync(path[, { throwIfNoEntry }])` → `size`, `mtimeMs`, `mtime`, `isFile()`, `isDirectory()`,
  `isSymbolicLink()`.

Encodings are `utf8` (or `utf-8`), `hex` and `base64`; Node's `base64url`, `latin1` and the rest are not there. A relative path is from the working directory of `ytrack`. A path that is
not a regular file fails (`EISDIR`, `EINVAL`) instead of blocking on a pipe. An error carries `code`, `syscall` and
`path`; left uncaught it prints as `bad_usage` with `path`. An option Node has and this list lacks is a `TypeError`.

## `Buffer`

Global, as in Node, from goja_nodejs: `Buffer.from`, `Buffer.alloc`, `Buffer.concat`, `Buffer.isBuffer`,
`buf.toString(encoding)` and the read/write methods of Node's `Buffer`.

## `fetch`

```js
const answer = fetch(url, { timeout: 10000, maxBytes: 5 * 1024 * 1024, redirect: "follow" });
if (answer.ok) fs.writeFileSync("image.png", Buffer.from(answer.arrayBuffer()));
```

- Synchronous: returns the response, no `Promise`. Only `GET` (`method` may only be `"GET"`), no request headers,
  only `http` and `https`.
- Required: `timeout` in milliseconds (≥ 1), `maxBytes` (≥ 0), `redirect` — `"follow"` (up to 10) or `"manual"`.
- The response: `status`, `ok`, `statusText`, `url`, `redirected`, `headers` (`get`, `has`, `forEach`, `keys`,
  `values`, `entries`, `getSetCookie`; names lower case), `text()`, `json()`, `arrayBuffer()`. The body is read whole,
  so each method may be called again.
- A status outside `2xx` is a response. A transport failure, the timeout, an eleventh redirect and a body longer than
  `maxBytes` throw `upstream_failed` with `url`.
- The instance token is never sent, even to `address`; no cookies, no proxy from the environment, no retry.

## Versions

- The version is the `N` of `require("ytrack/v<N>")`, unrelated to the release version; every module requires its
  own. Once `v2` is out, the current and the previous version are supported, and the previous one warns
  `script_failed` with the date it goes and the version to move to; today `v1` is the only one.
- A version breaks only by removing or renaming a function, a key, an own key of an answer or a code, changing their
  type or meaning, making an optional key required, or changing the exit-code rule. What the server returns for
  `fields=` is not the version's.
- `fs`, `Buffer` and `fetch` are Node's names, outside the versions; they only grow.
