/** Клиент к REST YouTrack и Hub — ровно столько, сколько нужно сидированию. */

/** Ответ приезжает под выражением вызывающего, поэтому дерево, а не структура. */
export type Tree = Record<string, any>;

export class ApiError extends Error {
  readonly status: number;

  constructor(method: string, url: string, status: number, body: string) {
    super(`${method} ${url} → ${status}: ${body}`);
    this.name = "ApiError";
    this.status = status;
  }
}

export class Api {
  private readonly baseUrl: string;
  private readonly token: string;

  constructor(baseUrl: string, token: string) {
    this.baseUrl = baseUrl.replace(/\/+$/, "");
    this.token = token;
  }

  private async call(
    method: string, path: string, fields?: string, body?: string | FormData, contentType?: string,
  ): Promise<any> {
    let url = this.baseUrl + path;
    if (fields) {
      // Запятая и скобки — синтаксис выражения полей, кодировать их нечего
      url += (url.includes("?") ? "&" : "?") + "fields=" + encodeURIComponent(fields).replaceAll("%2C", ",");
    }
    const headers: Record<string, string> = { Authorization: `Bearer ${this.token}`, Accept: "application/json" };
    if (contentType) headers["Content-Type"] = contentType;
    const response = await fetch(url, { method, headers, body });
    const text = await response.text();
    if (!response.ok) throw new ApiError(method, url, response.status, text);
    return text ? JSON.parse(text) : null;
  }

  get(path: string, fields?: string): Promise<any> {
    return this.call("GET", path, fields);
  }

  post(path: string, body: unknown, fields?: string): Promise<any> {
    return this.call("POST", path, fields, JSON.stringify(body), "application/json");
  }

  // Content-Type не задаётся: multipart-заголовок с boundary частей fetch ставит сам
  upload(path: string, form: FormData, fields?: string): Promise<any> {
    return this.call("POST", path, fields, form);
  }

  delete(path: string): Promise<any> {
    return this.call("DELETE", path);
  }
}
