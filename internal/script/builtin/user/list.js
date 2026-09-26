const { users, fail } = require("ytrack/v1");

exports.definition = {
  short: "Search users",
  long: "Search users by login or name prefix. An email address matches nothing.",
  flags: [
    { name: "query", type: "string", usage: "login or name `prefix`" },
    { name: "fields", type: "fields", default: "login,fullName,banned" },
    { name: "limit", type: "int", usage: "max users", default: 50 },
    { name: "skip", type: "int", usage: "users to pass over before the first", default: 0 },
  ],
};

exports.command = (flags) => {
  if (flags.query === undefined) {
    fail("bad_usage", 'no --query was given: it carries the text to search for, and --query "" finds every user');
  }
  return users.list({ query: flags.query, fields: flags.fields, limit: flags.limit, skip: flags.skip });
};
