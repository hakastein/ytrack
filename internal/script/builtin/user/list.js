const { user } = require("ytrack/v1");

exports.command = {
  short: "Search users",
  long: "Search users by login or name prefix. An email address matches nothing.",
  flags: [
    { name: "query", type: "string", usage: "login or name `prefix`" },
    { name: "fields", type: "fields", default: "login,fullName,banned" },
    { name: "limit", type: "int", usage: "max users", default: 50 },
    { name: "skip", type: "int", usage: "users to pass over before the first", default: 0 },
  ],
};

exports.run = (flags) => user.list(flags);
