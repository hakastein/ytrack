const { field } = require("ytrack/v1");

exports.command = {
  short: "Show a custom field",
  long: "Show a custom field with its allowed values.\n\nA field of users prints the people and groups of its bundle under bundle.values and every user they hold under bundle.aggregatedUsers, where an empty list means anyone.",
  args: [
    { name: "project", type: "string", usage: "short name of the project, such as DEV" },
    { name: "field", type: "string", usage: "name or localized name of the custom field" },
  ],
  flags: [
    { name: "fields", type: "fields", default: "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty,bundle(values(name,archived),aggregatedUsers(login))" },
  ],
};

exports.run = (project, name, flags) => field.show(project, name, flags);
