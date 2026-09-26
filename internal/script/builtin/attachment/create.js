const { attachment } = require("ytrack/v1");

exports.command = {
  short: "Attach a file",
  long: "Attach a local file.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
    { name: "path", type: "path", usage: "local file to attach" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "id,name,size,mimeType,url" },
  ],
};

exports.run = (owner, path, flags) => attachment.create(owner, path, flags);
