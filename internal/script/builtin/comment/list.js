const { comment } = require("ytrack/v1");

exports.command = {
  short: "List comments",
  long: "List comments, oldest first.\n\nComments of an article have no deleted. --limit keeps the oldest; ytrack issue show --comments 5 prints the latest five.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "id,author(login),created,text,deleted" },
    { name: "limit", type: "int", usage: "max comments", default: 50 },
    { name: "skip", type: "int", usage: "comments to pass over before the first", default: 0 },
  ],
};

exports.run = (owner, flags) => comment.list(owner, flags);
