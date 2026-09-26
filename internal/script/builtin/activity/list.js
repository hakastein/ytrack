const { activity } = require("ytrack/v1");

exports.command = {
  short: "List activities of an issue",
  long: "List activities of an issue, newest first.\n\nChanges of a description, a summary or a comment carry the whole text before and after. Narrow with --category, or leave added and removed out of --fields.\n\n--category takes AttachmentsCategory, CommentTextCategory, CommentsCategory, CustomFieldCategory, DescriptionCategory, IssueCreatedCategory, IssueResolvedCategory, LinksCategory, SummaryCategory, TagsCategory, VcsChangeCategory, WorkItemCategory.",
  args: [
    { name: "issue", type: "string", usage: "readable id of the issue, such as DEV-1" },
  ],
  flags: [
    { name: "category", type: "strings", usage: "`category` to print; repeatable; default all", choices: ["AttachmentsCategory", "CommentTextCategory", "CommentsCategory", "CustomFieldCategory", "DescriptionCategory", "IssueCreatedCategory", "IssueResolvedCategory", "LinksCategory", "SummaryCategory", "TagsCategory", "VcsChangeCategory", "WorkItemCategory"] },
    { name: "fields", type: "fields", default: "timestamp,author(login),category,field,added(id,idReadable,login,name,urls),removed(id,idReadable,login,name,urls)" },
    { name: "limit", type: "int", usage: "max activities", default: 50 },
    { name: "skip", type: "int", usage: "activities to pass over before the first", default: 0 },
  ],
};

exports.run = (issue, flags) => activity.list(issue, flags);
