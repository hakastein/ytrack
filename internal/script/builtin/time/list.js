const { time } = require("ytrack/v1");

exports.command = {
  short: "List work items",
  long: "List work items, oldest first.\n\ndate is a day, printed as its midnight UTC.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the issue, such as DEV-1" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "id,duration,type(name),attributes,author(login),date,text" },
    { name: "limit", type: "int", usage: "max work items", default: 50 },
    { name: "skip", type: "int", usage: "work items to pass over before the first", default: 0 },
  ],
};

exports.run = (issue, flags) => time.list(issue, flags);
