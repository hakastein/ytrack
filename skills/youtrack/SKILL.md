---
name: youtrack
description: Use for any interaction with YouTrack.
---

YouTrack is reached through the `ytrack` CLI: `ytrack <command> <subcommand>`, commands
`issue`, `activity`, `comment`, `link`, `tag`, `field`, `time`, `attachment`,
`article`, `project`, `user`, `auth`. A repository may add commands of its own in
`.ytrack/scripts`, and the user in `~/.ytrack/scripts`: `ytrack --help` lists them
under the directory they come from, and they print and fail the same way.

- An answer is one YAML document on stdout; an error is one on stderr, `code` first.
- An answer holds only the fields asked for — the command's default set or `--fields`.
  A field that is not printed was not requested; it is not empty.
- Exit `2` is `write_uncertain`, or a failure after a write: the instance may have
  changed. Read the object back before repeating it.
- `script_failed` is a defect of a command script, at its `file`, `line` and
  `column`: tell the person who owns the script, and do not repeat the call.
