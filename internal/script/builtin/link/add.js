const { link } = require("ytrack/v1");

exports.command = {
  short: "Link two issues",
  long: "Link two issues; prints the links of the first.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the first issue, such as DEV-1" },
    { name: "phrase", type: "string", usage: "phrase of the link read from the first issue to the second, such as \"depends on\"" },
    { name: "target", type: "string", usage: "readable id of the second issue, such as DEV-2" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "idReadable,summary" },
  ],
};

exports.run = (issue, phrase, target, flags) => link.add(issue, phrase, target, flags);
