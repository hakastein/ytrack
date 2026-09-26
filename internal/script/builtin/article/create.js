const { articles, fail } = require("ytrack/v1");

exports.definition = {
  short: "Create an article",
  long: "Create an article in a project.",
  args: [
    { name: "project", type: "string", usage: "short name of the project, such as DEV" },
  ],
  flags: [
    { name: "summary", type: "string", usage: "`title`" },
    { name: "content", type: "string", usage: "article `text`" },
    { name: "parent", type: "string", usage: "parent article `id`" },
    {
      name: "fields",
      type: "fields",
      default:
        "idReadable,summary,reporter(login),created,updated,tags(name)," +
        "parentArticle(idReadable,summary),childArticles(idReadable,summary),content",
    },
  ],
};

exports.command = (project, flags) => {
  if (flags.summary === undefined) {
    fail("bad_usage", "no --summary was given: it carries the title of the article, which YouTrack files none without");
  }
  if (flags.content === "") {
    fail("bad_usage", "--content is empty, and YouTrack keeps empty content as none: leave the flag out to file the article with no content at all");
  }
  if (flags.parent === "") {
    fail("bad_usage", "--parent names no article: leave the flag out to file the article at the root of its project");
  }
  return articles.create({
    project,
    summary: flags.summary,
    content: flags.content,
    parent: flags.parent,
    fields: flags.fields,
  });
};
