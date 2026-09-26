const { issues, fail } = require("ytrack/v1");

exports.definition = {
  short: "Show an issue",
  long:
    "Show an issue with comments, oldest first.\n\nCustom fields are named inside customFields by name " +
    "or localized name, in any letter case, quoted where they hold a space: --fields " +
    "'+customFields(Priority,\"Due Date\")'. A bare customFields prints every field that holds " +
    "something; a named field is printed even when empty, and one the issue does not have is left " +
    "out.\n\nComments print id, author(login), created and text whatever --fields says, and deleted ones " +
    "are left out.",
  args: [
    { name: "id", type: "string", usage: "readable id of the issue, such as DEV-1" },
  ],
  flags: [
    {
      name: "fields",
      type: "fields",
      default:
        "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields," +
        "links(issues(idReadable,summary)),description",
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
  issues.show({ id, fields: flags.fields, comments: commentCount(flags.comments) });
