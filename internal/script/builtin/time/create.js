const { workItems, fail } = require("ytrack/v1");

exports.definition = {
  short: "Log time",
  long:
    "Log time as you.\n\n--date is today if left out. --type is one of the workItemTypes ytrack project " +
    "show prints, --attribute one of its attributes.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the issue, such as DEV-1" },
    { name: "duration", type: "duration", usage: "ISO 8601 period of hours and minutes, such as PT1H30M" },
  ],
  flags: [
    { name: "date", type: "string", usage: "`day`, as in 2026-09-01" },
    { name: "type", type: "string", usage: "work item type `name`" },
    { name: "text", type: "string", usage: "work item `text`" },
    { name: "attribute", type: "pair", multiple: true, usage: "work item attribute `Name=value`; repeatable" },
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

exports.command = (issue, minutes, flags) => {
  if (flags.date === "") {
    fail("bad_usage", "--date names no day: leave the flag out to log the time today");
  }
  if (flags.type === "") {
    fail("bad_usage", "--type names no type of work: the types an issue may be written against are the settings of its project, printed by ytrack project show <code> under plugins");
  }
  return workItems.create({
    issue,
    minutes,
    date: flags.date,
    type: flags.type,
    text: flags.text,
    attributes: attributesOf(flags.attribute),
    fields: flags.fields,
  });
};
