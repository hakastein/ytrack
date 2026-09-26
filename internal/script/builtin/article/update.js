const { articles, fail } = require("ytrack/v1");

exports.definition = {
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
    {
      name: "fields",
      type: "fields",
      default:
        "idReadable,summary,reporter(login),created,updated,tags(name)," +
        "parentArticle(idReadable,summary),childArticles(idReadable,summary),content",
    },
  ],
};

exports.command = (id, flags) => {
  let content = flags.content;
  let parent = flags.parent;
  for (const part of flags.clear ?? []) {
    if (part.toLowerCase() === "content") {
      content = null;
    } else if (part.toLowerCase() === "parent") {
      parent = null;
    } else {
      fail("bad_usage", "--clear `" + part + "` names no part of an article a call may empty: it takes content or parent");
    }
  }
  return articles.update({ id, summary: flags.summary, content, parent, fields: flags.fields });
};
