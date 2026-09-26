const { project } = require("ytrack/v1");

exports.command = {
  short: "List projects",
  long: "List projects.",
  flags: [
    { name: "fields", type: "fields", default: "shortName,name" },
    { name: "limit", type: "int", default: 50, usage: "max projects" },
    { name: "skip", type: "int", default: 0, usage: "projects to pass over before the first" },
  ],
};

exports.run = (flags) => project.list(flags);
