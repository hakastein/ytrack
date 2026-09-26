const { projects } = require("ytrack/v1");

exports.definition = {
  short: "List projects",
  long: "List projects.",
  flags: [
    { name: "fields", type: "fields", default: "shortName,name" },
    { name: "limit", type: "int", default: 50, usage: "max projects" },
    { name: "skip", type: "int", default: 0, usage: "projects to pass over before the first" },
  ],
};

exports.command = (flags) => projects.list({ fields: flags.fields, limit: flags.limit, skip: flags.skip });
