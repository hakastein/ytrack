const { attachment } = require("ytrack/v1");

exports.command = {
  short: "Delete an attachment",
  long: "Delete an attachment. A file attached to a comment belongs to the issue.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
    { name: "id", type: "string", usage: "attachment id such as 12-1 that ytrack attachment list prints, not a file name" },
  ],
};

exports.run = (owner, id) => attachment.delete(owner, id);
