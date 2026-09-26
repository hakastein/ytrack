const { comment } = require("ytrack/v1");

exports.command = {
  short: "Delete a comment",
  long: "Delete a comment.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
    { name: "id", type: "string", usage: "comment id such as 7-1 that ytrack comment list prints" },
  ],
};

exports.run = (owner, id) => comment.delete(owner, id);
