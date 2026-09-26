// The Node subset of a command script: synchronous, and only what is declared here. It takes the place of
// @types/node, whose fetch returns a promise.

type BufferEncoding = "utf8" | "utf-8" | "hex" | "base64";

interface Buffer extends Uint8Array {
  toString(encoding?: BufferEncoding, start?: number, end?: number): string;
  equals(other: Uint8Array): boolean;
}

interface BufferConstructor {
  from(data: string, encoding?: BufferEncoding): Buffer;
  from(data: ArrayBuffer | Uint8Array | readonly number[]): Buffer;
  alloc(size: number, fill?: string | number, encoding?: BufferEncoding): Buffer;
  concat(list: readonly Uint8Array[], totalLength?: number): Buffer;
  isBuffer(value: unknown): value is Buffer;
}

declare var Buffer: BufferConstructor;

// timeout is in milliseconds; redirect "follow" follows up to 10 redirects.
interface FetchOptions {
  timeout: number;
  maxBytes: number;
  redirect: "follow" | "manual";
  method?: "GET" | "get";
}

// Names are lower case; a header sent more than once reads as its values joined by ", ".
interface FetchHeaders {
  get(name: string): string | null;
  has(name: string): boolean;
  forEach(each: (value: string, name: string, headers: FetchHeaders) => void): void;
  keys(): string[];
  values(): string[];
  entries(): [string, string][];
  getSetCookie(): string[];
}

// The body is read whole, so each method may be called again.
interface FetchResponse {
  readonly status: number;
  readonly ok: boolean;
  readonly statusText: string;
  readonly url: string;
  readonly redirected: boolean;
  readonly headers: FetchHeaders;
  text(): string;
  json(): unknown;
  arrayBuffer(): ArrayBuffer;
}

// Throws the fault upstream_failed, with url, on a transport failure, the timeout, an eleventh redirect and a body
// longer than maxBytes; a status outside 2xx is a response.
declare function fetch(url: string, options: FetchOptions): FetchResponse;

declare module "fs" {
  interface Stats {
    size: number;
    mtimeMs: number;
    mtime: Date;
    isFile(): boolean;
    isDirectory(): boolean;
    isSymbolicLink(): boolean;
  }

  // What a failure throws; left uncaught, it prints as bad_usage with path.
  interface SystemError extends Error {
    code: string;
    syscall: string;
    path: string;
  }

  function readFileSync(path: string, options?: null | { encoding?: null }): Buffer;
  function readFileSync(path: string, options: BufferEncoding | { encoding: BufferEncoding }): string;
  function writeFileSync(
    path: string,
    data: string | Uint8Array,
    options?: BufferEncoding | { encoding?: BufferEncoding },
  ): void;
  function mkdirSync(path: string, options?: { recursive?: false }): undefined;
  // The first directory made, or undefined when every level was there.
  function mkdirSync(path: string, options: { recursive: true }): string | undefined;
  function existsSync(path: unknown): boolean;
  function readdirSync(path: string, options?: "utf8" | "utf-8" | { encoding?: "utf8" | "utf-8" }): string[];
  function statSync(path: string, options?: { throwIfNoEntry?: true }): Stats;
  function statSync(path: string, options: { throwIfNoEntry: false }): Stats | undefined;
}

declare module "node:fs" {
  export * from "fs";
}

declare module "buffer" {
  const Buffer: BufferConstructor;
}

declare module "node:buffer" {
  const Buffer: BufferConstructor;
}
