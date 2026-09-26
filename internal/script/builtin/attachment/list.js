const { attachment } = require("ytrack/v1");

exports.command = {
  short: "List attachments",
  long: "List attachments, including those of comments; --fields +comment(id) says which comment.\n\nurl downloads the file without a token for up to three days: keep it as secret as a token.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "id,name,size,mimeType,url" },
    { name: "limit", type: "int", usage: "max attachments", default: 50 },
    { name: "skip", type: "int", usage: "attachments to pass over before the first", default: 0 },
  ],
};

exports.run = (owner, flags) => attachment.list(owner, flags);
