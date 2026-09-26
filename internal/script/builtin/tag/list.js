const { tag } = require("ytrack/v1");

exports.command = {
  short: "List tags",
  long: "List tags you own or that are shared with you. Names may clash across owners.",
  flags: [
    { name: "fields", type: "fields", default: "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login))" },
    { name: "limit", type: "int", usage: "max tags", default: 50 },
    { name: "skip", type: "int", usage: "tags to pass over before the first", default: 0 },
  ],
};

exports.run = (flags) => tag.list(flags);
