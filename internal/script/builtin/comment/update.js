const { comments, fail } = require("ytrack/v1");

exports.definition = {
  short: "Replace a comment's text",
  long:
    "Replace a comment's text.\n\n--fields +issue(...) on a comment of an article, or +article(...) on " +
    "one of an issue, is refused only after the comment is written.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
    { name: "id", type: "string", usage: "comment id such as 7-1 that ytrack comment list prints" },
  ],
  flags: [
    { name: "text", type: "string", usage: "comment `text`" },
    { name: "fields", type: "fields", default: "id,author(login),created,updated,text" },
  ],
};

exports.command = (owner, id, flags) => {
  if (flags.text === undefined) {
    fail("bad_usage", "no --text was given: it carries the text of the comment, which is the whole of what a comment is");
  }
  return comments.update({ owner, id, text: flags.text, fields: flags.fields });
};
