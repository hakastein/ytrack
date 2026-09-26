const { projects } = require("ytrack/v1");

exports.definition = {
  short: "Show a project",
  long: "Show a project.\n\nworkItemTypes are what ytrack time create --type takes.",
  args: [
    { name: "code", type: "string", usage: "short name of the project, such as DEV" },
  ],
  flags: [
    {
      name: "fields",
      type: "fields",
      default:
        "shortName,name,plugins(timeTrackingSettings(enabled,workItemTypes(name)))",
    },
  ],
};

exports.command = (code, flags) => projects.show({ project: code, fields: flags.fields });
