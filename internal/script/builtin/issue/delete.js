const { issue } = require("ytrack/v1");

exports.command = {
  short: "Delete an issue",
  long: "Delete an issue.",
  args: [
    { name: "id", type: "string", usage: "readable id of the issue, such as DEV-1" },
  ],
};

exports.run = (id) => issue.delete(id);
