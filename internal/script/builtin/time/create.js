const { time } = require("ytrack/v1");

exports.command = {
  short: "Log time",
  long: "Log time as you.\n\n--date is today if left out. --type is one of the workItemTypes ytrack project show prints, --attribute one of its attributes.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the issue, such as DEV-1" },
    { name: "duration", type: "string", usage: "ISO 8601 period of hours and minutes, such as PT1H30M" },
  ],
  flags: [
    { name: "date", type: "string", usage: "`day`, as in 2026-09-01" },
    { name: "type", type: "string", usage: "work item type `name`" },
    { name: "text", type: "string", usage: "work item `text`" },
    { name: "attribute", type: "strings", usage: "work item attribute `Name=value`; repeatable" },
    { name: "fields", type: "fields", default: "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields),text" },
  ],
};

exports.run = (issue, duration, flags) => time.create(issue, duration, flags);
