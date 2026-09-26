const { time } = require("ytrack/v1");

exports.command = {
  short: "Delete a work item",
  long: "Delete a work item.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the issue, such as DEV-1" },
    { name: "id", type: "string", usage: "work item id such as 150-1 that ytrack time list prints" },
  ],
};

exports.run = (issue, id) => time.delete(issue, id);
