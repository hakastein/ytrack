// Arguments in order, then an object of flags named as after --: ytrack project show DEV --fields name is
// project.show("DEV", { fields: "name" }), and a script of that command is run as exports.run("DEV", { fields: "name" }).
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

  // A function has no default values: every flag it takes whose command has a default is given, and the others may
  // be left out. A flag repeated on the command line is an array.
  interface Page {
    fields: string;
    limit: number;
    skip: number;
  }

  export const project: {
    show(code: string, flags: { fields: string }): Answer;
    list(flags: Page): Answer;
  };

  export const user: {
    show(login: string, flags: { fields: string }): Answer;
    list(flags: Page & { query?: string }): Answer;
  };

  export const field: {
    list(project: string, flags: { fields: string }): Answer;
    show(project: string, field: string, flags: { fields: string }): Answer;
  };

  export const issue: {
    show(id: string, flags: { fields: string; comments: string }): Answer;
    list(flags: Page & { query?: string }): Answer;
    create(
      project: string,
      flags: { fields: string; summary?: string; description?: string; field?: string[] },
    ): Answer;
    update(
      id: string,
      flags: { fields: string; summary?: string; description?: string; field?: string[]; clear?: string[] },
    ): Answer;
    delete(id: string): Answer;
  };

  export const article: {
    show(id: string, flags: { fields: string; comments: string }): Answer;
    list(flags: Page & { query?: string; parent?: string }): Answer;
    create(project: string, flags: { fields: string; summary?: string; content?: string; parent?: string }): Answer;
    update(
      id: string,
      flags: { fields: string; summary?: string; content?: string; parent?: string; clear?: string[] },
    ): Answer;
    delete(id: string): Answer;
  };

  export const comment: {
    list(owner: string, flags: Page): Answer;
    create(owner: string, flags: { fields: string; text?: string }): Answer;
    update(owner: string, id: string, flags: { fields: string; text?: string }): Answer;
    delete(owner: string, id: string): Answer;
  };

  export const attachment: {
    list(owner: string, flags: Page): Answer;
    // path is a local file, relative to the working directory of ytrack.
    create(owner: string, path: string, flags: { fields: string }): Answer;
    delete(owner: string, id: string): Answer;
  };

  export const link: {
    list(issue: string, flags: { fields: string }): Answer;
    add(issue: string, phrase: string, target: string, flags: { fields: string }): Answer;
    remove(issue: string, phrase: string, target: string): Answer;
  };

  export const tag: {
    list(flags: Page): Answer;
    create(
      flags: { fields: string; name?: string; "visible-for"?: string[]; "updateable-by"?: string[]; "taggable-by"?: string[] },
    ): Answer;
    delete(flags: { name?: string; "owned-by"?: string }): Answer;
    add(owner: string, flags: { name?: string; "owned-by"?: string }): Answer;
    remove(owner: string, flags: { name?: string; "owned-by"?: string }): Answer;
  };

  export const time: {
    list(issue: string, flags: Page): Answer;
    create(
      issue: string,
      duration: string,
      flags: { fields: string; date?: string; type?: string; text?: string; attribute?: string[] },
    ): Answer;
    update(
      issue: string,
      id: string,
      flags: {
        fields: string;
        duration?: string;
        date?: string;
        type?: string;
        text?: string;
        attribute?: string[];
        clear?: string[];
      },
    ): Answer;
    delete(issue: string, id: string): Answer;
  };

  export const activity: {
    list(issue: string, flags: Page & { category?: string[] }): Answer;
  };

  export function fail(code: Code, message: string, details?: { [key: string]: unknown }): never;
  export function warn(code: Code, message: string, details?: { [key: string]: unknown }): void;

  // The address of the login, its password masked. Reading it looks up the login.
  export const address: string;

  // exports.command: a pure literal, read without running the module.
  export interface Command {
    // One line, capitalized, no full stop, at most 60 characters.
    short: string;
    long: string;
    args?: Arg[];
    flags?: Flag[];
  }

  // Given to run in the order declared; a path completes as a file name.
  export interface Arg {
    name: string;
    type: "string" | "path";
    usage: string;
  }

  // Given to run in its last parameter under name; one without a default is there only when the call gives it.
  // strings is repeatable, int is 32 bits, fields is default when not given or empty and +expr adds expr to default.
  export type Flag =
    | { name: string; type: "string"; usage: string; choices?: string[]; default?: string }
    | { name: string; type: "strings"; usage: string; choices?: string[]; default?: string[] }
    | { name: string; type: "int"; usage: string; default?: number }
    | { name: string; type: "bool"; usage: string; default?: boolean }
    | { name: string; type: "fields"; default: string };
}
