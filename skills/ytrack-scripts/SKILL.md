---
name: ytrack-scripts
description: Use when writing or fixing a ytrack command script — a JavaScript file under `.ytrack/scripts` or `~/.ytrack/scripts`.
---

A command script is a JavaScript file that `ytrack` runs as a command of its own. Its sources of truth:

- the section [«Свои команды»](https://github.com/hakastein/ytrack#свои-команды) of the README — roots, how a file
  path becomes a command, the flag types, a whole script;
- [v1.d.ts](v1.d.ts) — `exports.definition` and every function of `require("ytrack/v1")`;
- [node.d.ts](node.d.ts) — `require("fs")`, `Buffer` and `fetch`, a synchronous subset of Node used in place of
  `@types/node`;
- the [built-in commands](https://github.com/hakastein/ytrack/tree/main/internal/script/builtin), scripts on the same
  API;
- `ytrack --help` and `ytrack <command> --help` for what the commands of this repository already do.

## Steps

1. **Declare the call.** `exports.definition` is a pure literal: `ytrack` parses it without running the module, so
   a name, a call or a computed value makes it unreadable; strings may be joined with `+`. Done when
   `ytrack <command> --help` prints the command and stderr holds no `script_failed`.
2. **Write `exports.command`,** synchronously: there is no event loop, so no `async` and no `await`, and `fetch`
   returns the response itself. A function of `ytrack/v1` gets `fields` whole — pass the `fields` flag on, which
   `ytrack` has already expanded.
3. **Fail with a code the agent already knows.** `fail` and `warn` take only the codes in `v1.d.ts`; a fault a
   function throws can be left to print as it is. Exit code `2` after a write is `ytrack`'s to count.

A `script_failed` names the `file`, `line` and `column` of the defect: the fix goes into the script there.
