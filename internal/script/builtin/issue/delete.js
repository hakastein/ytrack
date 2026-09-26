const { issues } = require("ytrack/v1");

exports.definition = {
  short: "Delete an issue",
  long: "Delete an issue.",
  args: [
    { name: "id", type: "string", usage: "readable id of the issue, such as DEV-1" },
  ],
};

exports.command = (id) => issues.delete({ id });
