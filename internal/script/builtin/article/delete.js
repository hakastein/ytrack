const { articles } = require("ytrack/v1");

exports.definition = {
  short: "Delete an article with its children",
  long: "Delete an article with its children.",
  args: [
    { name: "id", type: "string", usage: "readable id of the article, such as DEV-A-1" },
  ],
};

exports.command = (id) => articles.delete({ id });
