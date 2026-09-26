const { comment } = require("ytrack/v1");

exports.command = {
  short: "Add a comment",
  long: "Add a comment.\n\n--fields +issue(...) on a comment of an article, or +article(...) on one of an issue, is refused only after the comment is written.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
  ],
  flags: [
    { name: "text", type: "string", usage: "comment `text`" },
    { name: "fields", type: "fields", default: "id,author(login),created,updated,text" },
  ],
};

exports.run = (owner, flags) => comment.create(owner, flags);
