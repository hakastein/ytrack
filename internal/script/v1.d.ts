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

  // A function has no default values: every flag it takes is given.
  export namespace project {
    function show(code: string, flags: { fields: string }): Answer;
    function list(flags: { fields: string; limit: number; skip: number }): Answer;
  }

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
    // An answer of the command, printed after long in --help.
    example?: { [key: string]: unknown };
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
