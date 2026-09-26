const { field } = require("ytrack/v1");

exports.command = {
  short: "List custom fields of a project",
  long: "List custom fields of a project.",
  args: [
    { name: "project", type: "string", usage: "short name of the project, such as DEV" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty" },
  ],
};

exports.run = (project, flags) => field.list(project, flags);
