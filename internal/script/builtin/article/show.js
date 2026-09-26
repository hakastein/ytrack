const { article } = require("ytrack/v1");

exports.command = {
  short: "Show an article",
  long: "Show an article with comments, oldest first.",
  args: [
    { name: "id", type: "string", usage: "readable id of the article, such as DEV-A-1" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "idReadable,summary,reporter(login),created,updated,tags(name),parentArticle(idReadable,summary),childArticles(idReadable,summary),content" },
    { name: "comments", type: "string", usage: "latest comments to print: all or a `count`, 0 for none", default: "all" },
  ],
};

exports.run = (id, flags) => article.show(id, flags);
