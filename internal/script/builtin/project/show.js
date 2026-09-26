const { project } = require("ytrack/v1");

exports.command = {
  short: "Show a project",
  long: "Show a project.\n\nworkItemTypes are what ytrack time create --type takes.",
  args: [
    { name: "code", type: "string", usage: "short name of the project, such as DEV" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "shortName,name,plugins(timeTrackingSettings(enabled,workItemTypes(name)))" },
  ],
  example: {
    shortName: "DEV",
    name: "Project",
    plugins: { timeTrackingSettings: { enabled: true, workItemTypes: [{ name: "Type" }] } },
  },
};

exports.run = (code, flags) => project.show(code, flags);
