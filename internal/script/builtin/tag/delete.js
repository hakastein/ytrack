const { tags, fail } = require("ytrack/v1");

exports.definition = {
  short: "Delete a tag",
  long: "Delete a tag everywhere.\n\nAn administrator's token deletes a tag of another user as well.",
  flags: [
    { name: "name", type: "string", usage: "tag `name`" },
    { name: "owned-by", type: "string", usage: "owner `login`, when names clash" },
  ],
};

exports.command = (flags) => {
  if (flags["owned-by"] === "") {
    fail("bad_usage", "--owned-by is empty, and YouTrack keeps no user under an empty login: it takes the login of the user the tag belongs to");
  }
  return tags.delete({ name: flags.name ?? "", ownedBy: flags["owned-by"] });
};
