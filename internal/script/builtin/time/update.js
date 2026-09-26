const { workItems, fail } = require("ytrack/v1");

exports.definition = {
  short: "Update a work item",
  long: "Update a work item; unflagged parts stay.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the issue, such as DEV-1" },
    { name: "id", type: "string", usage: "work item id such as 150-1 that ytrack time list prints" },
  ],
  flags: [
    { name: "duration", type: "duration", usage: "`duration`, as in PT1H30M" },
    { name: "date", type: "string", usage: "`day`, as in 2026-09-01" },
    { name: "type", type: "string", usage: "work item type `name`" },
    { name: "text", type: "string", usage: "work item `text`" },
    { name: "attribute", type: "pair", multiple: true, usage: "work item attribute `Name=value`; repeatable" },
    { name: "clear", type: "string", multiple: true, usage: "empty a `part`: type, text or an attribute name" },
    {
      name: "fields",
      type: "fields",
      default:
        "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields)," +
        "text",
    },
  ],
};

function attributesOf(given) {
  const attributes = {};
  for (const [name, value] of Object.entries(given ?? {})) {
    if (Array.isArray(value)) {
      fail("bad_usage", "--attribute `" + name + "` is given " + value.length + " times, and an attribute holds one value");
    }
    attributes[name] = value;
  }
  return attributes;
}

exports.command = (issue, id, flags) => {
  const attributes = attributesOf(flags.attribute);
  let text = flags.text;
  let type = flags.type;
  for (const part of flags.clear ?? []) {
    const named = part.toLowerCase();
    if (named === "text") {
      text = null;
    } else if (named === "type") {
      type = null;
    } else if (named === "duration" || named === "date") {
      fail(
        "bad_usage",
        "--clear `" + part + "` names a part every work item holds: YouTrack answers one of null with " +
          "Field cannot be null, so there is no way to empty it; --" + named + " writes it afresh",
      );
    } else if (part === "") {
      fail("bad_usage", '--clear "" names nothing to empty: it takes type, text or the name of an attribute');
    } else {
      attributes[part] = null;
    }
  }
  return workItems.update({
    issue,
    id,
    minutes: flags.duration,
    date: flags.date,
    type,
    text,
    attributes,
    fields: flags.fields,
  });
};
