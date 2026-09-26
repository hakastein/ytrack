const { tag } = require("ytrack/v1");

exports.command = {
  short: "Create a tag",
  long: "Create a tag owned by you.\n\nWithout flags only the owner sees the tag. Only --taggable-by lets a group hang it.",
  flags: [
    { name: "name", type: "string", usage: "tag `name`" },
    { name: "visible-for", type: "strings", usage: "`group` that sees the tag; repeatable" },
    { name: "updateable-by", type: "strings", usage: "`group` that may edit the tag; repeatable" },
    { name: "taggable-by", type: "strings", usage: "`group` that may tag with it; repeatable" },
    { name: "fields", type: "fields", default: "name,owner(login),readSharingSettings(permittedGroups(name),permittedUsers(login)),updateSharingSettings(permittedGroups(name),permittedUsers(login)),tagSharingSettings(permittedGroups(name),permittedUsers(login))" },
  ],
};

exports.run = (flags) => tag.create(flags);
