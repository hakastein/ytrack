const { link } = require("ytrack/v1");

exports.command = {
  short: "Unlink two issues",
  long: "Unlink two issues.\n\nA link can be named from either end: DEV-1 \"depends on\" DEV-2 and DEV-2 \"is required for\" DEV-1 remove the same link. A state a workflow set when the link was made stays.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the first issue, such as DEV-1" },
    { name: "phrase", type: "string", usage: "phrase of the link read from the first issue to the second, such as \"depends on\"" },
    { name: "target", type: "string", usage: "readable id of the second issue, such as DEV-2" },
  ],
};

exports.run = (issue, phrase, target) => link.remove(issue, phrase, target);
