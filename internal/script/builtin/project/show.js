const { project } = require("ytrack/v1");
const { fieldsOf } = require("../fields.js");

exports.command = {
  short: "Show a project",
  long: "Show a project.\n\nworkItemTypes are what ytrack time create --type takes.",
  args: [{ name: "code", type: "string" }],
  flags: {
    fields: { type: "string", usage: "YouTrack fields `expression`; +expr adds to the default" },
  },
  example: {
    shortName: "DEV",
    name: "Project",
    plugins: { timeTrackingSettings: { enabled: true, workItemTypes: [{ name: "Type" }] } },
  },
};

const defaultFields = "shortName,name,plugins(timeTrackingSettings(enabled,workItemTypes(name)))";

exports.run = (input) => project.show(input.code, { fields: fieldsOf(input.fields, defaultFields) });
