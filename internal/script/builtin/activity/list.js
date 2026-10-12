const { activities } = require("ytrack/v1");

exports.definition = {
  short: "List activities of an issue or an article",
  long:
    "List activities of an issue or an article, newest first.\n\nChanges of a description, a summary, the content " +
    "of an article or a comment carry the whole text before and after. Narrow with --category, or leave added and " +
    "removed out of --fields.\n\n--category of an issue takes AttachmentsCategory, CommentTextCategory, " +
    "CommentsCategory, CustomFieldCategory, DescriptionCategory, IssueCreatedCategory, IssueResolvedCategory, " +
    "LinksCategory, SummaryCategory, TagsCategory, VcsChangeCategory, WorkItemCategory; of an article " +
    "ArticleCreatedCategory, ArticleDeletedCategory, ArticleRestoredCategory, ArticleSummaryCategory, " +
    "ArticleDescriptionCategory, ArticleProjectCategory, ArticleAttachmentsCategory, ArticleVisibilityCategory, " +
    "ArticleTagsCategory, ArticleCommentsCategory.",
  args: [
    { name: "owner", type: "string", usage: "readable id of the issue or article, such as DEV-1 or DEV-A-1" },
  ],
  flags: [
    { name: "category", type: "string", multiple: true, usage: "`category` to print; repeatable; default all", choices: ["AttachmentsCategory", "CommentTextCategory", "CommentsCategory", "CustomFieldCategory", "DescriptionCategory", "IssueCreatedCategory", "IssueResolvedCategory", "LinksCategory", "SummaryCategory", "TagsCategory", "VcsChangeCategory", "WorkItemCategory", "ArticleCreatedCategory", "ArticleDeletedCategory", "ArticleRestoredCategory", "ArticleSummaryCategory", "ArticleDescriptionCategory", "ArticleProjectCategory", "ArticleAttachmentsCategory", "ArticleVisibilityCategory", "ArticleTagsCategory", "ArticleCommentsCategory"] },
    {
      name: "fields",
      type: "fields",
      default:
        "timestamp,author(login),category,field,added(id,idReadable,login,name,urls)," +
        "removed(id,idReadable,login,name,urls)",
    },
    { name: "limit", type: "int", usage: "max activities", default: 50 },
    { name: "skip", type: "int", usage: "activities to pass over before the first", default: 0 },
  ],
};

exports.command = (owner, flags) =>
  activities.list({ owner, categories: flags.category, fields: flags.fields, limit: flags.limit, skip: flags.skip });
