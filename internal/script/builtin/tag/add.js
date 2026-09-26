const { tags, fail } = require("ytrack/v1");

exports.definition = {
  short: "Tag an issue or article",
  long: "Tag an issue or article.",
  args: [
    { name: "id", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
  ],
  flags: [
    { name: "name", type: "string", usage: "tag `name`" },
    { name: "owned-by", type: "string", usage: "owner `login`, when names clash" },
  ],
};

exports.command = (id, flags) => {
  if (flags["owned-by"] === "") {
    fail("bad_usage", "--owned-by is empty, and YouTrack keeps no user under an empty login: it takes the login of the user the tag belongs to");
  }
  return tags.add({ id, name: flags.name ?? "", ownedBy: flags["owned-by"] });
};
