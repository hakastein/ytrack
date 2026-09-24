---
name: youtrack
description: Use for any interaction with YouTrack.
---

YouTrack is reached through the `ytrack` CLI: `ytrack <entity> <verb>`, entities
`issue`, `activity`, `comment`, `link`, `tag`, `field`, `time`, `attachment`,
`article`, `project`, `user`, `auth`.

- An answer is one YAML document on stdout; a failure is one on stderr, `code` first.
- An answer holds only the fields asked for — the command's default set or `--fields`.
  A field that is not printed was not requested; it is not empty.
- Exit `2` is `write_uncertain`: the write reached the server and may or may not
  have landed. Read the entity back before repeating it.
