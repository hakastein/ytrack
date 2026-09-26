const { article } = require("ytrack/v1");

exports.command = {
  short: "Delete an article with its children",
  long: "Delete an article with its children.",
  args: [
    { name: "id", type: "string", usage: "readable id of the article, such as DEV-A-1" },
  ],
};

exports.run = (id) => article.delete(id);
