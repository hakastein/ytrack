const { article } = require("ytrack/v1");

exports.command = {
  short: "Update an article",
  long: "Update an article; unflagged parts stay.",
  args: [
    { name: "id", type: "string", usage: "readable id of the article, such as DEV-A-1" },
  ],
  flags: [
    { name: "summary", type: "string", usage: "`title`" },
    { name: "content", type: "string", usage: "article `text`" },
    { name: "parent", type: "string", usage: "parent article `id`" },
    { name: "clear", type: "string", multiple: true, usage: "empty a `part`: content or parent" },
    { name: "fields", type: "fields", default: "idReadable,summary,reporter(login),created,updated,tags(name),parentArticle(idReadable,summary),childArticles(idReadable,summary),content" },
  ],
};

exports.run = (id, flags) => article.update(id, flags);
