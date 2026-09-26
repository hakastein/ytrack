---
name: ytrack-scripts
description: Use when writing or fixing a ytrack command script — a JavaScript file under `.ytrack/scripts` or `~/.ytrack/scripts`.
---

A command script is a procedure over YouTrack that `ytrack` runs as a command of its own: the path of the file under
the script root is the command path, `.ytrack/scripts/docs/map.js` is `ytrack docs map`. The repository's root is the
nearest `.ytrack/scripts` above the working directory; `~/.ytrack/scripts` holds the user's own. A first word the
built-in commands use (`issue`, `tag`, …) is theirs: a script cannot add `issue close`.

Every rule of the API, `fs` and `fetch` is in [REFERENCE.md](REFERENCE.md); the signatures are in
[v1.d.ts](v1.d.ts) and [node.d.ts](node.d.ts). Read them before the first call of a function you have not used.

## Shape

```js
const { issues, comments } = require("ytrack/v1");

exports.definition = {
  short: "Take an issue into work",
  long: "Move an issue to In Progress and leave a comment when --note is given.",
  args: [{ name: "id", type: "string", usage: "readable id of the issue, such as DEV-1" }],
  flags: [
    { name: "note", type: "string", usage: "comment `text`" },
    { name: "fields", type: "fields", default: "idReadable,summary,customFields(State)" },
  ],
};

exports.command = (id, flags) => {
  issues.update({ id, customFields: { State: "In Progress" }, fields: "idReadable" });
  if (flags.note !== undefined) {
    comments.create({ owner: id, text: flags.note, fields: "id" });
  }
  return issues.show({ id, fields: flags.fields });
};
```

## Steps

1. **Declare the call.** `exports.definition` is a pure literal — strings, numbers, booleans, arrays and objects,
   texts joined by `+` and nothing else — since `ytrack` reads it without running the module for `--help` and every
   TAB. `short` is one present-tense line, capitalized, no full stop, at most 60 characters. Done when
   `ytrack <path> --help` prints the command with no `script_failed` warning.
2. **Write `exports.command`.** It takes the declared arguments in order, then the object of flags, and returns one
   object: that is the YAML document `ytrack` prints. Each function of `ytrack/v1` takes one object of named keys
   with `fields` given whole — pass the expression from the `fields` flag, never an empty one or one starting with
   `+`. Code runs synchronously: no `async`, no `await`.
3. **Fail in the dictionary.** A failure the caller can act on goes out through `fail(code, message, details)` with a
   code of the dictionary; `warn` has the same form and goes to stderr. A fault a function throws may be caught — its
   `wrote` says whether the instance may have changed — or left to print as it is.
4. **Run it against a server of your own.** `YTRACK_URL` and `YTRACK_TOKEN` point `ytrack` at any HTTP server, so a
   test answers the requests the script sends and checks stdout, stderr and the exit code. Done when every branch of
   the script has run once: each flag given and left out, each fault it catches.

A `script_failed` names the `file`, `line` and `column` of the defect: fix the script there, the call may be right.
