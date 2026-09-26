const { links } = require("ytrack/v1");

exports.definition = {
  short: "List links of an issue",
  long:
    "List links of an issue by phrase.\n\nA phrase reads from this issue to the linked ones.\n\n--fields " +
    "applies to the linked issues.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the issue, such as DEV-1" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "idReadable,summary" },
  ],
};

exports.command = (issue, flags) => links.list({ issue, fields: flags.fields });
