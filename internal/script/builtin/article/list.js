const { article } = require("ytrack/v1");

exports.command = {
  short: "Search articles",
  long: "Search articles.\n\n--query takes the search attributes of articles: project or in, title, content, author, article id, tag, created, updated, updater, has, sort by. An attribute of issues, such as summary, finds nothing, and a query the server cannot parse finds every article, neither with an error.\n\n--parent lists the children of an article instead, and takes no --query.",
  flags: [
    { name: "query", type: "string", usage: "YouTrack `search`" },
    { name: "parent", type: "string", usage: "parent article `id`" },
    { name: "fields", type: "fields", default: "idReadable,summary" },
    { name: "limit", type: "int", usage: "max articles", default: 50 },
    { name: "skip", type: "int", usage: "articles to pass over before the first", default: 0 },
  ],
};

exports.run = (flags) => article.list(flags);
