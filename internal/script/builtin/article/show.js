const { articles, fail } = require("ytrack/v1");

exports.definition = {
  short: "Show an article",
  long: "Show an article with comments, oldest first.",
  args: [
    { name: "id", type: "string", usage: "readable id of the article, such as DEV-A-1" },
  ],
  flags: [
    {
      name: "fields",
      type: "fields",
      default:
        "idReadable,summary,reporter(login),created,updated,tags(name)," +
        "parentArticle(idReadable,summary),childArticles(idReadable,summary),content",
    },
    { name: "comments", type: "string", usage: "latest comments to print: all or a `count`, 0 for none", default: "all" },
  ],
};

function commentCount(given) {
  if (given === "all") {
    return "all";
  }
  if (!/^[0-9]+$/.test(given)) {
    fail("bad_usage", "--comments `" + given + "` is neither all nor a whole number of comments");
  }
  const count = Number(given);
  if (count > 2147483647) {
    fail("bad_usage", "--comments `" + given + "` is a larger number than there could ever be comments");
  }
  return count;
}

exports.command = (id, flags) =>
  articles.show({ id, fields: flags.fields, comments: commentCount(flags.comments) });
