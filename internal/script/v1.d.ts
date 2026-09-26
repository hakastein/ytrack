declare module "ytrack/v1" {
  // A value of an answer. A map and a list are read-only and print as the answer printed them. A number the double
  // cannot hold exactly is a bigint. A key the answer does not hold reads as undefined, one it holds empty as null.
  export type Value = string | number | bigint | boolean | null | Answer | readonly Value[];
  export interface Answer {
    readonly [key: string]: Value | undefined;
  }

  // The codes a script may fail or warn with. script_failed is ytrack's own, about a defect of a script.
  export type Code =
    | "bad_usage"
    | "unknown_name"
    | "missing_required"
    | "not_found"
    | "denied"
    | "rejected"
    | "upstream_failed"
    | "upstream_invalid"
    | "write_uncertain";

  // What a command function, fail and address throw. Uncaught, it is printed as the fault of the command.
  export interface Fault extends Error {
    readonly code: Code | "script_failed";
    readonly message: string;
    readonly details: Answer;
    // The instance may have changed.
    readonly wrote: boolean;
  }

  // fields is the whole expression of the answer, with no default; limit left out is the SDK's default page.
  interface Page {
    fields: string;
    limit?: number;
    skip?: number;
  }

  // "all" or the count of the latest comments; none when left out.
  type Comments = "all" | number;

  // A key left out is not written, and null empties the field: so for every part of a write.
  type CustomFields = { [name: string]: string | string[] | null };

  export interface FieldType {
    readonly valueType: string;
    readonly isMultiValue: boolean;
  }
  export interface Issue {
    readonly id: string;
    readonly idReadable: string;
    readonly summary: string;
    readonly description: string;
    readonly project: { readonly id: string; readonly shortName: string; readonly name: string };
    readonly fields: readonly {
      readonly name: string;
      readonly localizedName: string;
      readonly type: FieldType;
      readonly values: readonly { readonly id: string; readonly text: string; readonly localizedName: string }[];
    }[];
    readonly links: readonly {
      readonly direction: string;
      readonly type: { readonly name: string; readonly sourceToTarget: string; readonly targetToSource: string };
      readonly issues: readonly { readonly id: string; readonly idReadable: string }[];
    }[];
  }
  export interface ProjectField {
    readonly id: string;
    readonly name: string;
    readonly localizedName: string;
    readonly type: FieldType;
    readonly canBeEmpty: boolean;
  }
  export interface Metadata {
    readonly fields: readonly ProjectField[];
    readonly fromCache: boolean;
  }
  export interface Bundle {
    readonly field: ProjectField;
    readonly values: readonly { readonly id: string; readonly name: string; readonly archived: boolean }[];
  }
  export interface User {
    readonly id: string;
    readonly login: string;
    readonly fullName: string;
    readonly email: string;
    readonly banned: boolean;
  }

  export const issues: {
    show(call: { id: string; fields: string; comments?: Comments }): Answer;
    list(call: Page & { query: string }): Answer;
    create(call: {
      project: string;
      summary: string;
      description?: string;
      customFields?: CustomFields;
      fields: string;
    }): Answer;
    update(call: {
      id: string;
      summary?: string;
      description?: string | null;
      customFields?: CustomFields;
      fields: string;
    }): Answer;
    delete(call: { id: string }): Answer;
    get(call: { id: string }): Issue;
    writeFields(call: { id: string; customFields: CustomFields }): Issue;
  };

  export const articles: {
    show(call: { id: string; fields: string; comments?: Comments }): Answer;
    list(call: Page & { query: string }): Answer;
    children(call: Page & { parent: string }): Answer;
    create(call: { project: string; summary: string; content?: string; parent?: string; fields: string }): Answer;
    update(call: {
      id: string;
      summary?: string;
      content?: string | null;
      parent?: string | null;
      fields: string;
    }): Answer;
    delete(call: { id: string }): Answer;
  };

  // owner is the readable id of an issue or an article.
  export const comments: {
    list(call: Page & { owner: string }): Answer;
    create(call: { owner: string; text: string; fields: string }): Answer;
    update(call: { owner: string; id: string; text: string; fields: string }): Answer;
    delete(call: { owner: string; id: string }): Answer;
  };

  export const attachments: {
    list(call: Page & { owner: string }): Answer;
    // path is a local file, relative to the working directory of ytrack.
    create(call: { owner: string; path: string; fields: string }): Answer;
    delete(call: { owner: string; id: string }): Answer;
  };

  // phrase reads from issue to target, such as "depends on".
  export const links: {
    list(call: { issue: string; fields: string }): Answer;
    add(call: { issue: string; phrase: string; target: string; fields: string }): Answer;
    remove(call: { issue: string; phrase: string; target: string }): Answer;
  };

  // id is the readable id of the issue or the article a tag hangs on; ownedBy the login of the owner of the tag.
  export const tags: {
    list(call: Page): Answer;
    create(call: {
      name: string;
      visibleFor?: string[];
      updatableBy?: string[];
      taggableBy?: string[];
      fields: string;
    }): Answer;
    delete(call: { name: string; ownedBy?: string }): Answer;
    add(call: { id: string; name: string; ownedBy?: string }): Answer;
    remove(call: { id: string; name: string; ownedBy?: string }): Answer;
  };

  // A work item is whole minutes; date is a day, as 2026-09-01.
  export const workItems: {
    list(call: Page & { issue: string }): Answer;
    create(call: {
      issue: string;
      minutes: number;
      date?: string;
      type?: string;
      text?: string;
      attributes?: { [name: string]: string };
      fields: string;
    }): Answer;
    update(call: {
      issue: string;
      id: string;
      minutes?: number;
      date?: string;
      type?: string | null;
      text?: string | null;
      attributes?: { [name: string]: string | null };
      fields: string;
    }): Answer;
    delete(call: { issue: string; id: string }): Answer;
  };

  export const activities: {
    list(call: Page & { issue: string; categories?: string[] }): Answer;
  };

  export const projects: {
    show(call: { project: string; fields: string }): Answer;
    list(call: Page): Answer;
  };

  // name is the name or the localized name of a custom field of the project.
  export const customFields: {
    list(call: { project: string; fields: string }): Answer;
    show(call: { project: string; name: string; fields: string }): Answer;
    // From the metadata cache when it holds the project; readMetadata reads the server.
    metadata(call: { project: string }): Metadata;
    readMetadata(call: { project: string }): Metadata;
    bundle(call: { project: string; name: string }): Bundle;
  };

  export const users: {
    show(call: { login: string; fields: string }): Answer;
    list(call: Page & { query: string }): Answer;
    me(): User;
    find(call: { query: string; limit?: number }): readonly User[];
  };

  export function fail(code: Code, message: string, details?: { [key: string]: unknown }): never;
  export function warn(code: Code, message: string, details?: { [key: string]: unknown }): void;

  // The address of the login, its password masked. Reading it looks up the login.
  export const address: string;

  // exports.definition: a pure literal, read without running the module; a text may be strings joined by +.
  // exports.command takes the arguments in the order declared, then an object of the flags by name.
  export interface Definition {
    // One line, capitalized, no full stop, at most 60 characters.
    short: string;
    long: string;
    args?: Arg[];
    flags?: Flag[];
  }

  // A path completes as a file name; a duration, as PT1H30M, is given as its minutes.
  export interface Arg {
    name: string;
    type: "string" | "path" | "duration";
    usage: string;
  }

  // A flag without a default is absent unless given. Name=value is { Name: "value" }, a name repeated an array;
  // int is 32 bits; fields is default when empty, and +expr adds expr to default.
  export type Flag =
    | { name: string; type: "string"; multiple?: false; usage: string; choices?: string[]; default?: string }
    | { name: string; type: "string"; multiple: true; usage: string; choices?: string[]; default?: string[] }
    | { name: string; type: "pair"; multiple?: boolean; usage: string }
    | { name: string; type: "duration"; usage: string }
    | { name: string; type: "int"; usage: string; default?: number }
    | { name: string; type: "bool"; usage: string; default?: boolean }
    | { name: string; type: "fields"; default: string };
}
