const { customFields } = require("ytrack/v1");

exports.definition = {
  short: "List custom fields of a project",
  long: "List custom fields of a project.",
  args: [
    { name: "project", type: "string", usage: "short name of the project, such as DEV" },
  ],
  flags: [
    {
      name: "fields",
      type: "fields",
      default:
        "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty",
    },
  ],
};

exports.command = (project, flags) => customFields.list({ project, fields: flags.fields });
