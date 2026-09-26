const { project } = require("ytrack/v1");
const { fieldsOf } = require("../fields.js");
const { pageOf } = require("../page.js");

exports.command = {
  short: "List projects",
  long: "List projects.",
  flags: {
    fields: { type: "string", usage: "YouTrack fields `expression`; +expr adds to the default" },
    limit: { type: "int", usage: "max projects, 50 if left out" },
    skip: { type: "int", usage: "projects to pass over before the first" },
  },
  example: {
    total: 1,
    returned: 1,
    truncated: false,
    projects: [{ shortName: "DEV", name: "Project" }],
  },
};

const defaultFields = "shortName,name";

exports.run = (input) => project.list({ fields: fieldsOf(input.fields, defaultFields), ...pageOf(input) });
