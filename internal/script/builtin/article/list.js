const { articles, fail } = require("ytrack/v1");

exports.definition = {
  short: "Search articles",
  long:
    "Search articles.\n\n--query takes the search attributes of articles: project or in, title, content, " +
    "author, article id, tag, created, updated, updater, has, sort by. An attribute of issues, such as " +
    "summary, finds nothing, and a query the server cannot parse finds every article, neither with an " +
    "error.\n\n--parent lists the children of an article instead, and takes no --query.",
  flags: [
    { name: "query", type: "string", usage: "YouTrack `search`" },
    { name: "parent", type: "string", usage: "parent article `id`" },
    { name: "fields", type: "fields", default: "idReadable,summary" },
    { name: "limit", type: "int", usage: "max articles", default: 50 },
    { name: "skip", type: "int", usage: "articles to pass over before the first", default: 0 },
  ],
};

exports.command = (flags) => {
  if (flags.parent === undefined) {
    if (flags.query === undefined) {
      fail("bad_usage", 'no --query was given: it carries the search to run, and --query "" finds every article');
    }
    return articles.list({ query: flags.query, fields: flags.fields, limit: flags.limit, skip: flags.skip });
  }
  if (flags.query !== undefined) {
    fail("bad_usage", "--parent and --query were both given: the children of an article are listed with no search");
  }
  return articles.children({ parent: flags.parent, fields: flags.fields, limit: flags.limit, skip: flags.skip });
};
