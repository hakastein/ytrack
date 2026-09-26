const { issues } = require("ytrack/v1");

exports.definition = {
  short: "Update an issue",
  long:
    "Update an issue; unflagged parts stay.\n\n--field on a field of several values leaves it holding " +
    "exactly the values given, not the old ones plus them.",
  args: [
    { name: "id", type: "string", usage: "readable id of the issue, such as DEV-1" },
  ],
  flags: [
    { name: "summary", type: "string", usage: "`title`" },
    { name: "description", type: "string", usage: "`text` of the description" },
    { name: "field", type: "pair", multiple: true, usage: "custom field `Name=value`; repeatable" },
    { name: "clear", type: "string", multiple: true, usage: "empty a custom field or description by `name`" },
    {
      name: "fields",
      type: "fields",
      default:
        "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields," +
        "links(issues(idReadable,summary)),description",
    },
  ],
};

exports.command = (id, flags) => {
  const customFields = { ...flags.field };
  let description = flags.description;
  for (const name of flags.clear ?? []) {
    if (name.toLowerCase() === "description") {
      description = null;
    } else {
      customFields[name] = null;
    }
  }
  return issues.update({ id, summary: flags.summary, description, customFields, fields: flags.fields });
};
