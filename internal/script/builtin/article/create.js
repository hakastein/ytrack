const { article } = require("ytrack/v1");

exports.command = {
  short: "Create an article",
  long: "Create an article in a project.",
  args: [
    { name: "project", type: "string", usage: "short name of the project, such as DEV" },
  ],
  flags: [
    { name: "summary", type: "string", usage: "`title`" },
    { name: "content", type: "string", usage: "article `text`" },
    { name: "parent", type: "string", usage: "parent article `id`" },
    { name: "fields", type: "fields", default: "idReadable,summary,reporter(login),created,updated,tags(name),parentArticle(idReadable,summary),childArticles(idReadable,summary),content" },
  ],
};

exports.run = (project, flags) => article.create(project, flags);
