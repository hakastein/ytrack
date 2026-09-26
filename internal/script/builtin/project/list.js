const { project } = require("ytrack/v1");

exports.command = {
  short: "List projects",
  long: "List projects.",
  flags: [
    { name: "fields", type: "fields", default: "shortName,name" },
    { name: "limit", type: "int", default: 50, usage: "max projects" },
    { name: "skip", type: "int", default: 0, usage: "projects to pass over before the first" },
  ],
  example: {
    total: 1,
    returned: 1,
    truncated: false,
    projects: [{ shortName: "DEV", name: "Project" }],
  },
};

exports.run = (flags) => project.list(flags);
