const { time } = require("ytrack/v1");

exports.command = {
  short: "Update a work item",
  long: "Update a work item; unflagged parts stay.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the issue, such as DEV-1" },
    { name: "id", type: "string", usage: "work item id such as 150-1 that ytrack time list prints" },
  ],
  flags: [
    { name: "duration", type: "string", usage: "`duration`, as in PT1H30M" },
    { name: "date", type: "string", usage: "`day`, as in 2026-09-01" },
    { name: "type", type: "string", usage: "work item type `name`" },
    { name: "text", type: "string", usage: "work item `text`" },
    { name: "attribute", type: "string", multiple: true, usage: "work item attribute `Name=value`; repeatable" },
    { name: "clear", type: "string", multiple: true, usage: "empty a `part`: type, text or an attribute name" },
    { name: "fields", type: "fields", default: "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields),text" },
  ],
};

exports.run = (issue, id, flags) => time.update(issue, id, flags);
