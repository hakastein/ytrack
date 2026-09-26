const { issue } = require("ytrack/v1");

exports.command = {
  short: "Search issues",
  long: "Search issues.\n\nCustom fields are named inside customFields by name or localized name, in any letter case, quoted where they hold a space: --fields '+customFields(Priority,\"Due Date\")'. A bare customFields prints every field that holds something; a named field is printed even when empty, and one the issue does not have is left out. Links print under their phrase: --fields '+links(issues(idReadable))'.",
  flags: [
    { name: "query", type: "string", usage: "YouTrack `search`" },
    { name: "fields", type: "fields", default: "idReadable,summary,resolved,created" },
    { name: "limit", type: "int", usage: "max issues", default: 50 },
    { name: "skip", type: "int", usage: "issues to pass over before the first", default: 0 },
  ],
};

exports.run = (flags) => issue.list(flags);
