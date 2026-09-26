const { issue } = require("ytrack/v1");

exports.command = {
  short: "Update an issue",
  long: "Update an issue; unflagged parts stay.\n\n--field on a field of several values leaves it holding exactly the values given, not the old ones plus them.",
  args: [
    { name: "id", type: "string", usage: "readable id of the issue, such as DEV-1" },
  ],
  flags: [
    { name: "summary", type: "string", usage: "`title`" },
    { name: "description", type: "string", usage: "`text` of the description" },
    { name: "field", type: "strings", usage: "custom field `Name=value`; repeatable" },
    { name: "clear", type: "strings", usage: "empty a custom field or description by `name`" },
    { name: "fields", type: "fields", default: "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields,links(issues(idReadable,summary)),description" },
  ],
};

exports.run = (id, flags) => issue.update(id, flags);
