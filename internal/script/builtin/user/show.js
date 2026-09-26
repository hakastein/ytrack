const { user } = require("ytrack/v1");

exports.command = {
  short: "Show a user",
  long: "Show a user.",
  args: [
    { name: "login", type: "string", usage: "login, not a full name; ytrack user list --query finds one by name" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "login,fullName,email,banned" },
  ],
};

exports.run = (login, flags) => user.show(login, flags);
