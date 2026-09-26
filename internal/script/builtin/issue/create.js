const { issue } = require("ytrack/v1");

exports.command = {
  short: "Create an issue",
  long: "Create an issue in a project.",
  args: [
    { name: "project", type: "string", usage: "short name of the project, such as DEV" },
  ],
  flags: [
    { name: "summary", type: "string", usage: "`title`" },
    { name: "description", type: "string", usage: "`text` of the description" },
    { name: "field", type: "string", multiple: true, usage: "custom field `Name=value`; repeatable" },
    { name: "fields", type: "fields", default: "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields,links(issues(idReadable,summary)),description" },
  ],
};

exports.run = (project, flags) => issue.create(project, flags);
