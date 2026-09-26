const { tag } = require("ytrack/v1");

exports.command = {
  short: "Untag an issue or article",
  long: "Untag an issue or article. The tag stays.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
  ],
  flags: [
    { name: "name", type: "string", usage: "tag `name`" },
    { name: "owned-by", type: "string", usage: "owner `login`, when names clash" },
  ],
};

exports.run = (owner, flags) => tag.remove(owner, flags);
