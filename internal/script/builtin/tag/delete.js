const { tag } = require("ytrack/v1");

exports.command = {
  short: "Delete a tag",
  long: "Delete a tag everywhere.\n\nAn administrator's token deletes a tag of another user as well.",
  flags: [
    { name: "name", type: "string", usage: "tag `name`" },
    { name: "owned-by", type: "string", usage: "owner `login`, when names clash" },
  ],
};

exports.run = (flags) => tag.delete(flags);
