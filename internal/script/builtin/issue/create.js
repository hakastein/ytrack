const { issues, fail } = require("ytrack/v1");

exports.definition = {
  short: "Create an issue",
  long: "Create an issue in a project.",
  args: [
    { name: "project", type: "string", usage: "short name of the project, such as DEV" },
  ],
  flags: [
    { name: "summary", type: "string", usage: "`title`" },
    { name: "description", type: "string", usage: "`text` of the description" },
    { name: "field", type: "pair", multiple: true, usage: "custom field `Name=value`; repeatable" },
    {
      name: "fields",
      type: "fields",
      default:
        "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields," +
        "links(issues(idReadable,summary)),description",
    },
  ],
};

exports.command = (project, flags) => {
  if (flags.summary === undefined) {
    fail("bad_usage", "no --summary was given: it carries the title of the issue, which YouTrack files none without");
  }
  if (flags.description === "") {
    fail("bad_usage", "--description is empty, and YouTrack keeps an empty description as none: leave the flag out to file the issue with no description at all");
  }
  return issues.create({
    project,
    summary: flags.summary,
    description: flags.description,
    customFields: flags.field,
    fields: flags.fields,
  });
};
