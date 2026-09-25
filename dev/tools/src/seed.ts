/**
 * Сидирование dev-инстанса: проект DEV с кастом-полями всех типов каталога
 * и DOCS с выключенным учётом времени.
 *
 * Чистый YouTrack — это пустота, а контрактным тестам нужен полигон, на котором
 * исполняются обе ветки выбора `$type` и все двадцать строк таблицы типов.
 */

import { chmodSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import {
  ALL_TYPES_ISSUE, ARTICLE_TREE, ATTACHMENT, COMMENT, COMMON_VALUES, DOCS, COPY_LINK, HISTORY, HISTORY_EDIT_KINDS, ISSUES,
  LINKS, TIME_TRACKING, WORK_ITEM, WORK_ITEM_ATTRIBUTE, WORK_ITEM_TYPES, type Article, type FieldValue, type HistoryEdit, type Issue,
  type WorkItem,
} from "./content.ts";
import {
  DOCS_FIELD_ORDER, DOCS_FIELDS, FIELD_ORDER, FIELDS, valueType, type Field, type TypeId, type ValueType,
} from "./fields.ts";
import { INSTANCE_TIME_ZONE, LIMITED, MEMBER, type DevInstanceUser } from "./users.ts";
import { Api, ApiError, type Tree } from "./youtrack.ts";

const BASE_URL = process.env.YOUTRACK_URL!.replace(/\/+$/, "");
const TOKEN_FILE = process.env.YOUTRACK_TOKEN_FILE ?? "/state/admin-token";
const LIMITED_TOKEN_FILE = join(dirname(TOKEN_FILE), "limited-token");
const MEMBER_TOKEN_FILE = join(dirname(TOKEN_FILE), "member-token");

type DevInstanceProject = { key: string; name: string; fields: Field[]; order: string[]; workflows: string[] };
const DEV_PROJECT: DevInstanceProject = {
  key: "DEV", name: "DEVELOPMENT", fields: FIELDS, order: FIELD_ORDER,
  // Каскад владельца `Subsystem` в `Assignee` и голос за `+1` в комментарии исполняют воркфлоу, а не сервер
  workflows: ["Subsystem Assignee", "One Vote Comment"],
};
const DOCS_PROJECT: DevInstanceProject = {
  key: "DOCS", name: "DOCS", fields: DOCS_FIELDS, order: DOCS_FIELD_ORDER, workflows: [],
};
const PROJECTS = [DEV_PROJECT, DOCS_PROJECT];
const ADMIN_LOGIN = process.env.YOUTRACK_ADMIN_LOGIN!;
const LIMITED_PASSWORD = process.env.YOUTRACK_LIMITED_PASSWORD ?? "ytrack-dev";
const MEMBER_PASSWORD = process.env.YOUTRACK_MEMBER_PASSWORD ?? "ytrack-dev";
const DEV_INSTANCE_USERS = [ADMIN_LOGIN, LIMITED.login, MEMBER.login];
const VALUE_OWNER = ADMIN_LOGIN;
const VALUE_OWNER_RULE = `владелец значения — только ${VALUE_OWNER}: поля привязываются раньше, чем заводятся ` +
  "остальные пользователи полигона";
// Команду проекта YouTrack заводит сам, группой `<имя проекта> Team`
const DEV_INSTANCE_GROUPS = [`${DEV_PROJECT.name} Team`, MEMBER.group];
// Проект у Hub один, поэтому роль участника глобальная. В команду DEV REST добавляет только группу, а её
// пользователи попадают в `Assignee`
const HUB_PROJECT = "GLBL";
const READ_ISSUE = "JetBrains.YouTrack.READ_ISSUE";
// На их отсутствии стоят 403 участнику на правку чужого комментария и на чтение групп
const WITHHELD_PERMISSIONS = ["JetBrains.YouTrack.UPDATE_NOT_OWN_COMMENT", "jetbrains.jetpass.group-read"];
const POLL_INTERVAL = 500;
const POLL_TIMEOUT = 60_000;
const SUBSYSTEM_CASCADE = { source: "Subsystem", target: "Assignee" };
// Воркфлоу на инстансе больше 42, а без `$top` сервер отдаёт только первые 42
const WORKFLOW_CATALOG = "/api/admin/workflows?$top=-1";
// Выражение из правила One Vote Comment: им он режет текст комментария на слова
const VOTE_WORD_SEPARATOR = /\s|,|;|\.|\?|!|\\/;
const VOTE_WORD = "+1";

// Тип значения он же путь к бандлам: /api/admin/customFieldSettings/bundles/<тип>; у `null` бандла со значениями нет
const BUNDLE_TYPE: Record<ValueType, string | null> = {
  enum: "EnumBundle",
  state: "StateBundle",
  version: "VersionBundle",
  build: "BuildBundle",
  ownedField: "OwnedBundle",
  user: "UserBundle",
  group: null,
  text: null,
  period: null,
  date: null,
  "date and time": null,
  integer: null,
  float: null,
  string: null,
};

const PROJECT_FIELD_TYPE: Record<ValueType, string> = {
  enum: "EnumProjectCustomField",
  state: "StateProjectCustomField",
  version: "VersionProjectCustomField",
  build: "BuildProjectCustomField",
  ownedField: "OwnedProjectCustomField",
  user: "UserProjectCustomField",
  group: "GroupProjectCustomField",
  text: "TextProjectCustomField",
  period: "PeriodProjectCustomField",
  date: "SimpleProjectCustomField",
  "date and time": "SimpleProjectCustomField",
  integer: "SimpleProjectCustomField",
  float: "SimpleProjectCustomField",
  string: "SimpleProjectCustomField",
};

const ISSUE_FIELD_TYPE: Record<TypeId, string> = {
  "enum[1]": "SingleEnumIssueCustomField",
  "enum[*]": "MultiEnumIssueCustomField",
  "state[1]": "StateIssueCustomField",
  "version[1]": "SingleVersionIssueCustomField",
  "version[*]": "MultiVersionIssueCustomField",
  "build[1]": "SingleBuildIssueCustomField",
  "build[*]": "MultiBuildIssueCustomField",
  "ownedField[1]": "SingleOwnedIssueCustomField",
  "ownedField[*]": "MultiOwnedIssueCustomField",
  "user[1]": "SingleUserIssueCustomField",
  "user[*]": "MultiUserIssueCustomField",
  "group[1]": "SingleGroupIssueCustomField",
  "group[*]": "MultiGroupIssueCustomField",
  date: "DateIssueCustomField",
  "date and time": "SimpleIssueCustomField",
  integer: "SimpleIssueCustomField",
  float: "SimpleIssueCustomField",
  string: "SimpleIssueCustomField",
  text: "TextIssueCustomField",
  period: "PeriodIssueCustomField",
};

// Ключ, под которым значение пишется в тело и приходит в ответе; дата, число и строка идут без обёртки
const VALUE_KEY: Record<ValueType, string | null> = {
  enum: "name", state: "name", version: "name", build: "name", ownedField: "name", group: "name",
  user: "login", text: "text", period: "minutes",
  date: null, "date and time": null, integer: null, float: null, string: null,
};

const isString = (value: unknown): boolean => typeof value === "string";

// Вид значения поля без бандла, как его описывает `FieldValue`; значения бандлов, логины и группы сверяет `value_unlisted`
const VALUE_KIND: Record<ValueType, { kind: string; fits: (value: unknown) => boolean } | null> = {
  enum: null, state: null, version: null, build: null, ownedField: null, user: null, group: null,
  date: { kind: "момент в мс", fits: Number.isInteger },
  "date and time": { kind: "момент в мс", fits: Number.isInteger },
  integer: { kind: "целое", fits: Number.isInteger },
  float: { kind: "число", fits: Number.isFinite },
  string: { kind: "строка", fits: isString },
  text: { kind: "строка", fits: isString },
  period: { kind: "минуты", fits: Number.isInteger },
};

// Состав полигона печатается из этих записей, а не из дерева ответа: имя поля между записью и печатью сверяет tsc
type TimeTracking = { enabled: boolean; estimate: string | null; timeSpent: string | null; workItemTypes: string[] };
type CreatedIssue = { id: string; idReadable: string; description: string | null; values: FieldValue[] };
type CreatedLink = { from: string; phrase: string; to: string };
type CreatedWorkItem = { issue: string; author: string; type: string; minutes: number; text: string; date: number };
type CreatedAttachment = { issue: string; name: string; size: number };
type CreatedComment = { id: string; issue: string; author: string; text: string };
type CreatedArticle = { idReadable: string; parent: string | null };
type FieldDefault = { field: string; default: string; required: boolean };
type RequiredWhenShown = { field: string; when: { field: string; value: string } };
type ValueOwner = { field: string; value: string; owner: string };
type AttachedFields = { attached: Map<string, Tree>; defaults: FieldDefault[]; owners: ValueOwner[] };
type CreatedMember = { login: string; email: string; role: string; project: string; group: string; timeZone: string };
type CheckedLimited = { login: string; roles: string[]; projects: string[] };
type IssueHistory = { issue: string; categories: Record<string, number> };
type Vote = { issue: string; voter: string };
/** Запись истории в сверяемых ключах: ключ, которого нет, не сверяется */
type Activity = { category: string; author?: string; added?: unknown; removed?: unknown };
type FieldOrder = { project: string; fields: string[] };

function fail(code: string, text: string): never {
  console.error(`${code} ${text}`);
  process.exit(1);
}

function unreachable(value: never): never {
  throw new Error(`не разобрано ${JSON.stringify(value)}`);
}

function divergence(want: Tree, got: Tree): string[] {
  return Object.entries(want)
    .filter(([key, value]) => got[key] !== value)
    .map(([key, value]) => `${key} ${JSON.stringify(got[key])} вместо ${JSON.stringify(value)}`);
}

const fieldOf = (project: DevInstanceProject, name: string): Field | undefined =>
  project.fields.find((f) => f.name === name);

const writtenValues = (issue: Issue): FieldValue[] =>
  [...COMMON_VALUES, { field: "State", value: issue.state }, ...(issue.values ?? [])];

const isEmpty = (value: unknown): boolean => value === null || value === "" || (Array.isArray(value) && !value.length);

function cascadeOwner(project: DevInstanceProject, values: FieldValue[]): string | undefined {
  const source = values.find((v) => v.field === SUBSYSTEM_CASCADE.source);
  return fieldOf(project, SUBSYSTEM_CASCADE.source)?.values?.find((v) => v.name === source?.value)?.owner;
}

/** Таблицы ссылаются друг на друга, а разрешаются ссылки посреди записей, когда проект уже заведён */
function checkReferences(): void {
  const referenced: [table: string, issue: Issue][] = [
    ...LINKS.flatMap(([from, , to]): [string, Issue][] => [["LINKS", from], ["LINKS", to]]),
    ["WORK_ITEM", WORK_ITEM.issue], ["ATTACHMENT", ATTACHMENT.issue], ["COMMENT", COMMENT.issue],
    ["HISTORY", HISTORY.issue], ["ALL_TYPES_ISSUE", ALL_TYPES_ISSUE],
  ];
  const unlisted = [...new Set(referenced.filter(([, issue]) => !ISSUES.includes(issue))
    .map(([table, issue]) => `${table} ${JSON.stringify(issue.summary)}`))];
  if (unlisted.length) {
    fail("issue_unlisted", `таблицы полигона ссылаются на задачи, которых нет в ISSUES: ${unlisted.join(", ")}`);
  }
  const fieldNames = new Set(DEV_PROJECT.fields.map((f) => f.name));
  const absent = Object.values(TIME_TRACKING).filter((name) => !fieldNames.has(name))
    .map((name) => JSON.stringify(name));
  if (absent.length) {
    fail("time_tracking_field_unlisted", `учёт времени называет поля, которых нет в FIELDS: ${absent.join(", ")}`);
  }
  const workItems: [table: string, item: WorkItem][] = [["WORK_ITEM", WORK_ITEM], ["HISTORY", HISTORY.memberWorkItem]];
  const unlistedTypes = workItems
    .filter(([, item]) => !WORK_ITEM_TYPES.some((t) => !t.globalOnly && t.name === item.type.name))
    .map(([table, item]) => `${table} ${JSON.stringify(item.type.name)}`);
  if (unlistedTypes.length) {
    fail("work_item_type_unlisted",
      `записи времени называют типы, которых нет в наборе ${DEV_PROJECT.key}: ${unlistedTypes.join(", ")}`);
  }
  const unlistedDocsTypes = DOCS.workItemTypes.filter((type) => !WORK_ITEM_TYPES.some((t) => t.name === type.name))
    .map((type) => JSON.stringify(type.name));
  if (unlistedDocsTypes.length) {
    fail("work_item_type_unlisted",
      `набор ${DOCS_PROJECT.key} называет типы, которых нет в таблице типов работ: ${unlistedDocsTypes.join(", ")}`);
  }
}

function checkFields(): void {
  const named = PROJECTS.flatMap((project) => project.fields.map((field) => ({ project, field })));
  const conflicting = [...new Set(named.map(({ field }) => field.name))]
    .map((name) => named.filter(({ field }) => field.name === name))
    .filter((same) => new Set(same.map(({ field }) => field.typeId)).size > 1)
    .map((same) => `${JSON.stringify(same[0].field.name)}: ` +
      same.map(({ project, field }) => `${project.key} ${field.typeId}`).join(", "));
  if (conflicting.length) {
    fail("prototype_type_conflict",
      `у одного имени поля в таблицах проектов разные типы, а прототип у имени один: ${conflicting.join("; ")}`);
  }
  const unlistedDefaults = PROJECTS.flatMap((project) => project.fields
    .filter((f) => f.defaultValue !== undefined && !f.values?.some((v) => v.name === f.defaultValue))
    .map((f) => `${project.key} ${f.name} ${JSON.stringify(f.defaultValue)}`));
  if (unlistedDefaults.length) {
    fail("default_value_unlisted", `умолчания полей не из их бандлов: ${unlistedDefaults.join(", ")}`);
  }
  const foreignOwners = PROJECTS.flatMap((project) => project.fields.flatMap((f) => (f.values ?? [])
    .filter((v) => v.owner !== undefined && v.owner !== VALUE_OWNER)
    .map((v) => `${project.key} ${f.name} ${JSON.stringify(v.name)} ${JSON.stringify(v.owner)}`)));
  if (foreignOwners.length) {
    fail("owner_unlisted", `владельцы значений не ${VALUE_OWNER}: ${foreignOwners.join(", ")}; ${VALUE_OWNER_RULE}`);
  }
  const unconditioned = PROJECTS.flatMap((project) => project.fields
    .filter((f) => f.requiredWhenShown && !f.condition).map((f) => `${project.key} ${f.name}`));
  if (unconditioned.length) {
    fail("required_condition_unlisted", `обязательны под условием поля без условия: ${unconditioned.join(", ")}`);
  }
  const { key, fields } = DEV_PROJECT;
  const defaulted = fields.filter((f) => f.defaultValue !== undefined);
  if (!defaulted.some((f) => f.required)) {
    fail("required_default_missing", `у ${key} нет обязательного поля с умолчанием`);
  }
  if (!defaulted.some((f) => !f.required)) {
    fail("optional_default_missing", `у ${key} нет необязательного поля с умолчанием`);
  }
  if (!fields.some((f) => f.requiredWhenShown && f.condition)) {
    fail("required_condition_missing", `у ${key} нет поля, обязательного под условием`);
  }
}

function checkFieldOrder(): void {
  // Раньше непокрытых: опечатка в имени оставляет без места и настоящее поле
  const unlisted = PROJECTS.flatMap((project) => project.order
    .filter((name) => !project.fields.some((f) => f.name === name))
    .map((name) => `${project.key} ${JSON.stringify(name)}`));
  if (unlisted.length) {
    fail("field_order_unlisted", `порядок полей называет поля не из таблицы своего проекта: ${unlisted.join(", ")}`);
  }
  const repeated = PROJECTS.flatMap((project) => [...new Set(project.order
    .filter((name, i) => project.order.indexOf(name) !== i))]
    .map((name) => `${project.key} ${JSON.stringify(name)}`));
  if (repeated.length) {
    fail("field_order_duplicate", `порядок полей называет поля дважды: ${repeated.join(", ")}`);
  }
  const uncovered = PROJECTS.flatMap((project) => project.fields
    .filter((f) => !project.order.includes(f.name))
    .map((f) => `${project.key} ${JSON.stringify(f.name)}`));
  if (uncovered.length) {
    fail("field_order_uncovered", `у полей нет места в порядке своего проекта: ${uncovered.join(", ")}`);
  }
}

/** Выпавшая из описания черта не ломает ни типов, ни сверки с ответом: описание сверяется с той же константой */
function checkTextFeatures(): void {
  const missing = ISSUES.flatMap((issue) => (issue.textFeatures ?? [])
    .filter(([, pattern]) => !pattern.test(issue.description ?? ""))
    .map(([name]) => `${JSON.stringify(issue.summary)}: ${name}`));
  if (missing.length) {
    fail("text_feature_missing", `описания полигона не несут черт: ${missing.join(", ")}`);
  }
}

function calendarDay(moment: number, timeZone: string): string {
  const parts = new Intl.DateTimeFormat("en", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" })
    .formatToParts(moment);
  return ["year", "month", "day"].map((type) => parts.find((part) => part.type === type)!.value).join("-");
}

function checkTimeZoneFixture(): void {
  if (MEMBER.timeZone === INSTANCE_TIME_ZONE) {
    fail("time_zone_fixture_inert", `пояс участника ${MEMBER.timeZone} совпадает с поясом инстанса`);
  }
  // День записи времени сервер берёт в поясе профиля того, чей токен пишет, а не в поясе инстанса
  const { date, storedDate } = HISTORY.memberWorkItem;
  const member = calendarDay(date, MEMBER.timeZone);
  const instance = calendarDay(date, INSTANCE_TIME_ZONE);
  const stored = calendarDay(storedDate, "UTC");
  if (member === instance || member !== stored) {
    fail("time_zone_fixture_inert", `момент записи участника ${new Date(date).toISOString()} приходится на ${member} ` +
      `в его поясе и на ${instance} в поясе инстанса, а хранение ожидается ${stored}`);
  }
}

function checkHistoryTable(): void {
  const kinds = new Set(HISTORY.edits.map((edit) => edit.kind));
  const uncovered = HISTORY_EDIT_KINDS.filter((kind) => !kinds.has(kind));
  if (uncovered.length) {
    fail("history_uncovered", `в истории ${JSON.stringify(HISTORY.issue.summary)} нет правок: ${uncovered.join(", ")}`);
  }
  const inert = HISTORY.edits.filter((edit) => "from" in edit && edit.from === edit.to).map((edit) => edit.kind);
  if (inert.length) {
    fail("history_edit_inert", `правки истории не меняют значения: ${inert.join(", ")}`);
  }
  if (!HISTORY.memberComment.split(VOTE_WORD_SEPARATOR).includes(VOTE_WORD)) {
    fail("vote_token_missing",
      `в комментарии участника ${JSON.stringify(HISTORY.memberComment)} нет отдельного слова ${VOTE_WORD}`);
  }
}

function listed(project: DevInstanceProject, { field, value }: FieldValue): boolean {
  const typed = fieldOf(project, field);
  if (!typed) return false;
  const type = valueType(typed);
  const names = type === "user" ? DEV_INSTANCE_USERS : type === "group" ? DEV_INSTANCE_GROUPS
    : BUNDLE_TYPE[type] !== null ? (typed.values ?? []).map((v) => v.name) : null;
  if (!names) return true;
  const items: unknown = typed.typeId.endsWith("[*]") ? value : [value];
  return Array.isArray(items) && items.every((item) => names.includes(item));
}

function checkValueTable(): void {
  const described = (issue: Issue, { field, value }: FieldValue): string =>
    `${JSON.stringify(issue.summary)} ${field} ${JSON.stringify(value)}`;
  const repeated = ISSUES.flatMap((issue) => {
    const fields = writtenValues(issue).map((v) => v.field);
    return [...new Set(fields.filter((field, i) => fields.indexOf(field) !== i))]
      .map((field) => `${JSON.stringify(issue.summary)} ${field}`);
  });
  if (repeated.length) {
    fail("value_repeated", `значения задачи из COMMON_VALUES, State и values называют поле дважды: ${repeated.join(", ")}`);
  }
  const empty = ISSUES.flatMap((issue) => writtenValues(issue).filter((v) => isEmpty(v.value))
    .map((v) => described(issue, v)));
  if (empty.length) {
    fail("value_empty", `пустые значения задач: ${empty.join(", ")}`);
  }
  const unlisted = ISSUES.flatMap((issue) => writtenValues(issue).filter((v) => !listed(DEV_PROJECT, v))
    .map((v) => described(issue, v)));
  if (unlisted.length) {
    fail("value_unlisted",
      `значения задач не из бандлов своих полей, пользователей или групп полигона: ${unlisted.join(", ")}`);
  }
  const misfit = ISSUES.flatMap((issue) => writtenValues(issue).flatMap((v) => {
    const kind = VALUE_KIND[valueType(fieldOf(DEV_PROJECT, v.field)!)];
    return kind === null || kind.fits(v.value) ? [] : [`${described(issue, v)} (ожидается ${kind.kind})`];
  }));
  if (misfit.length) {
    fail("value_kind_mismatch", `значения задач не того вида, что их поле: ${misfit.join(", ")}`);
  }
  const { summary } = ALL_TYPES_ISSUE;
  const values = writtenValues(ALL_TYPES_ISSUE);
  const { source, target } = SUBSYSTEM_CASCADE;
  const receiving = values.filter((v) => !isEmpty(v.value)).map((v) => v.field);
  if (receiving.includes(source)) receiving.push(target);
  const covered = new Set(receiving.map((name) => fieldOf(DEV_PROJECT, name)?.typeId));
  const uncovered = [...new Set(DEV_PROJECT.fields.map((f) => f.typeId))].filter((id) => !covered.has(id)).sort();
  if (uncovered.length) {
    fail("values_uncovered", `у ${JSON.stringify(summary)} нет непустых значений полей типов: ${uncovered.join(", ")}`);
  }
  if (cascadeOwner(DEV_PROJECT, values) === undefined) {
    fail("cascade_owner_missing", `у значения ${source} задачи ${JSON.stringify(summary)} нет владельца, ` +
      `и ${target} ей каскад не поставит`);
  }
  if (values.some((v) => v.field === target)) {
    fail("cascade_owner_missing",
      `${target} задачи ${JSON.stringify(summary)} записан явно, а каскад от ${source} ставит его только в пустой`);
  }
}

/** Таблица типов — двадцать строк, и полигон обязан исполнять каждую. */
async function checkCoverage(api: Api): Promise<void> {
  const catalog = new Set<string>(
    (await api.get("/api/admin/customFieldSettings/types", "id")).map((t: Tree) => t.id),
  );
  const seeded = new Set<string>(DEV_PROJECT.fields.map((f) => f.typeId));
  const missing = [...catalog].filter((id) => !seeded.has(id)).sort();
  if (missing.length) {
    fail("types_uncovered", `инстанс публикует типы, которых нет в полигоне: ${missing.join(", ")}`);
  }
  const requested = new Set(PROJECTS.flatMap((project) => project.fields.map((f) => f.typeId)));
  const unknown = [...requested].filter((id) => !catalog.has(id)).sort();
  if (unknown.length) {
    fail("types_unknown", `полигон просит типы, которых инстанс не знает: ${unknown.join(", ")}`);
  }
}

/** Резолв до проекта: пропавший предопределённый прототип валит сидирование раньше первой записи */
async function resolvePrototypes(api: Api): Promise<Map<string, string>> {
  const catalog: Tree[] = await api.get("/api/admin/customFieldSettings/customFields", "id,name");
  const ids = new Map<string, string>();
  const missing: string[] = [];
  const predefined = new Set(PROJECTS.flatMap((project) =>
    project.fields.filter((f) => f.predefined).map((f) => f.name)));
  for (const name of predefined) {
    const found = catalog.find((p) => p.name === name);
    if (found) ids.set(name, found.id);
    else missing.push(JSON.stringify(name));
  }
  if (missing.length) {
    fail("prototype_missing", `инстанс не завёл предопределённые поля: ${missing.join(", ")}`);
  }
  return ids;
}

async function checkWorkflows(api: Api): Promise<void> {
  const catalog: Tree[] = await api.get(WORKFLOW_CATALOG, "title");
  const missing = [...new Set(PROJECTS.flatMap((project) => project.workflows))]
    .filter((title) => !catalog.some((w) => w.title === title))
    .map((title) => JSON.stringify(title));
  if (missing.length) {
    fail("workflow_missing", `в каталоге инстанса нет воркфлоу: ${missing.join(", ")}`);
  }
}

type HubRole = { role: string; project: string };

async function resolveMemberRole(api: Api): Promise<HubRole> {
  const { roles } = await api.get("/hub/api/rest/roles?$top=-1", "id,key");
  const { projects } = await api.get("/hub/api/rest/projects?$top=-1", "id,key");
  const role: Tree | undefined = (roles as Tree[]).find((r) => r.key === MEMBER.role);
  const project: Tree | undefined = (projects as Tree[]).find((p) => p.key === HUB_PROJECT);
  if (!role) fail("member_role_missing", `в Hub нет роли ${JSON.stringify(MEMBER.role)}`);
  if (!project) fail("member_role_missing", `в Hub нет проекта ${JSON.stringify(HUB_PROJECT)}`);
  return { role: role.id, project: project.id };
}

async function checkInstanceTimeZone(api: Api): Promise<void> {
  const appearance: Tree = await api.get("/api/admin/globalSettings/appearanceSettings", "timeZone(id)");
  // На поясе профиля admin стоит дата записи времени DEV-1
  const admin: Tree = await api.get("/api/users/me", "profiles(general(timezone(id)))");
  const diverged = divergence({ instance: INSTANCE_TIME_ZONE, admin: INSTANCE_TIME_ZONE }, {
    instance: appearance.timeZone?.id ?? null, admin: admin.profiles?.general?.timezone?.id ?? null,
  });
  if (diverged.length) {
    fail("time_zone_mismatch", `пояс инстанса и профиля ${ADMIN_LOGIN} расходится с таблицей: ${diverged.join(", ")}`);
  }
}

/**
 * Мастер заводит пять типов работ, а недостающие до каталога полигона заводятся здесь. Уже заведённые
 * сверяются, а не заводятся заново: второй тип с тем же именем сервер отвергает.
 */
async function ensureWorkItemTypes(api: Api): Promise<Map<string, Tree>> {
  const path = "/api/admin/timeTrackingSettings/workItemTypes";
  const fields = "id,name,autoAttached";
  const catalog = new Map<string, Tree>((await api.get(path, fields)).map((t: Tree) => [t.name, t]));
  const missing = WORK_ITEM_TYPES.filter((t) => t.predefined && !catalog.has(t.name))
    .map((t) => JSON.stringify(t.name));
  if (missing.length) {
    fail("work_item_type_missing", `инстанс не завёл типы работ: ${missing.join(", ")}`);
  }
  // Раньше непокрытых: опечатка в имени типа мастера делает непокрытым настоящий тип
  const named = new Set(WORK_ITEM_TYPES.map((t) => t.name));
  const uncovered = [...catalog.keys()].filter((name) => !named.has(name)).sort()
    .map((name) => JSON.stringify(name));
  if (uncovered.length) {
    fail("work_item_types_uncovered", `инстанс публикует типы работ, которых нет в полигоне: ${uncovered.join(", ")}`);
  }
  const diverged = WORK_ITEM_TYPES.filter((t) => catalog.has(t.name)).flatMap((t) =>
    divergence({ autoAttached: t.autoAttached }, catalog.get(t.name)!).map((d) => `${JSON.stringify(t.name)} ${d}`));
  if (diverged.length) {
    fail("work_item_type_mismatch", `уже заведённые типы работ расходятся с полигоном: ${diverged.join(", ")}`);
  }
  for (const type of WORK_ITEM_TYPES.filter((t) => !catalog.has(t.name))) {
    const body = { name: type.name, autoAttached: type.autoAttached };
    const created: Tree = await api.post(path, body, fields);
    const wrong = divergence(body, created);
    if (wrong.length) {
      fail("work_item_type_mismatch",
        `заведённый сейчас тип работ ${JSON.stringify(type.name)} расходится с полигоном: ${wrong.join(", ")}`);
    }
    catalog.set(type.name, created);
  }
  return catalog;
}

/**
 * Уже заведённый тип сверяется, а не заводится заново: `make seed` после отказа на
 * старте застаёт его на инстансе, а второй тип с тем же именем сервер отвергает.
 */
async function ensureLinkType(api: Api): Promise<void> {
  const fields = Object.keys(COPY_LINK).join(",");
  const catalog: Tree[] = await api.get("/api/issueLinkTypes", fields);
  const existing = catalog.find((t) => t.name === COPY_LINK.name);
  const type: Tree = existing ?? await api.post("/api/issueLinkTypes", COPY_LINK, fields);
  const diverged = divergence(COPY_LINK, type);
  if (diverged.length) {
    const origin = existing ? "уже заведённый" : "заведённый сейчас";
    fail("link_type_mismatch", `${origin} тип связи ${COPY_LINK.name} расходится с полигоном: ${diverged.join(", ")}`);
  }
}

async function checkLinkCoverage(api: Api): Promise<number> {
  const catalog: Tree[] = await api.get("/api/issueLinkTypes", "name,sourceToTarget,targetToSource");
  const touched = new Set<string>();
  const unknown: string[] = [];
  for (const [, phrase] of LINKS) {
    // Пустой `targetToSource` у Relates фразой не считается
    const named = catalog.filter((t) =>
      phrase !== "" && (t.sourceToTarget === phrase || t.targetToSource === phrase));
    if (named.length === 1) touched.add(named[0].name);
    else unknown.push(JSON.stringify(phrase));
  }
  // Раньше непокрытых: фраза с опечаткой оставляет незадетым и свой тип
  if (unknown.length) {
    fail("links_unknown", `фразы полигона не называют ровно один тип связи: ${unknown.join(", ")}`);
  }
  const uncovered = catalog.map((t) => t.name).filter((name) => !touched.has(name)).sort();
  if (uncovered.length) {
    fail("links_uncovered", `инстанс публикует типы связей, которых нет в полигоне: ${uncovered.join(", ")}`);
  }
  return catalog.length;
}

async function createProject(api: Api, project: DevInstanceProject, leaderId: string): Promise<string> {
  const existing: Tree[] = await api.get("/api/admin/projects", "id,shortName");
  if (existing.some((p) => p.shortName === project.key)) {
    fail("project_exists", `проект ${project.key} уже есть: инстанс сидирован, нужен \`make reset && make install\``);
  }
  const created = await api.post("/api/admin/projects", {
    name: project.name, shortName: project.key, leader: { id: leaderId },
  }, "id,customFields(id)");
  // Проект приезжает с восемью полями по умолчанию: набор полигона задаёт только таблица
  for (const attached of created.customFields as Tree[]) {
    await api.delete(`/api/admin/projects/${created.id}/customFields/${attached.id}`);
  }
  return created.id;
}

async function createPrototype(api: Api, field: Field): Promise<string> {
  const created = await api.post("/api/admin/customFieldSettings/customFields", {
    name: field.name, fieldType: { id: field.typeId },
  }, "id");
  return created.id;
}

async function makeBundle(
  api: Api, project: DevInstanceProject, field: Field, ownerIds: ReadonlyMap<string, string>,
): Promise<[bundle: Tree, owners: ValueOwner[]] | null> {
  const type = valueType(field);
  const bundleType = BUNDLE_TYPE[type];
  if (bundleType === null || !field.values?.length) return null;
  const path = `/api/admin/customFieldSettings/bundles/${type}`;
  const bundle = await api.post(path, { name: `${field.name} (${project.key})` }, "id");
  const owners: ValueOwner[] = [];
  for (const { owner, ...value } of field.values) {
    if (owner === undefined) {
      await api.post(`${path}/${bundle.id}/values`, value, "id");
      continue;
    }
    const described = `${project.key} ${field.name} ${JSON.stringify(value.name)}`;
    const ownerId = ownerIds.get(owner)
      ?? fail("owner_unlisted", `у владельца ${described} ${JSON.stringify(owner)} нет id; ${VALUE_OWNER_RULE}`);
    const created: Tree = await api.post(`${path}/${bundle.id}/values`, { ...value, owner: { id: ownerId } },
      "name,owner(login)");
    const got = { name: created.name, owner: created.owner?.login ?? null };
    const diverged = divergence({ name: value.name, owner }, got);
    if (diverged.length) {
      fail("owner_mismatch", `значение ${described} расходится с записанным: ${diverged.join(", ")}`);
    }
    owners.push({ field: field.name, value: got.name, owner: got.owner });
  }
  // Без `$type` привязка к проекту отвечает 500 java.lang.InstantiationException
  return [{ id: bundle.id, $type: bundleType }, owners];
}

async function attach(
  api: Api, project: DevInstanceProject, projectId: string, prototypes: Map<string, string>,
  ownerIds: ReadonlyMap<string, string>, field: Field,
): Promise<[attached: Tree, owners: ValueOwner[]]> {
  const type = valueType(field);
  // Прототип у имени поля один на все проекты: заведённый здесь берут одноимённые поля следующих проектов
  const prototype = prototypes.get(field.name) ?? await createPrototype(api, field);
  prototypes.set(field.name, prototype);
  const body: Tree = {
    $type: PROJECT_FIELD_TYPE[type],
    field: { id: prototype },
    canBeEmpty: !field.required,
    emptyFieldText: "-",
  };
  const made = await makeBundle(api, project, field, ownerIds);
  if (made) body.bundle = made[0];
  if (BUNDLE_TYPE[type] !== null) {
    // Предопределённый прототип тянет значение по умолчанию из штатного
    // бандла, и свой бандл отвергается тем, что его не содержит
    body.defaultValues = [];
  }
  const attached: Tree = await api.post(`/api/admin/projects/${projectId}/customFields`, body,
    "id,$type,field(name),canBeEmpty,bundle(id,values(id,name,$type))");
  if (attached.canBeEmpty !== body.canBeEmpty) {
    fail("field_required_mismatch",
      `canBeEmpty ${project.key} ${field.name} ${JSON.stringify(attached.canBeEmpty)} вместо ${body.canBeEmpty}`);
  }
  return [attached, made ? made[1] : []];
}

async function applyCondition(
  api: Api, projectId: string, attached: Map<string, Tree>, field: Field,
): Promise<void> {
  const host = attached.get(field.condition!.field)!;
  const value = host.bundle.values.find((v: Tree) => v.name === field.condition!.value);
  await api.post(`/api/admin/projects/${projectId}/customFields/${attached.get(field.name)!.id}`, {
    condition: {
      $type: "FieldBasedCondition",
      field: { id: host.id, $type: host.$type },
      values: [{ id: value.id, $type: value.$type }],
    },
  });
}

async function applyDefault(
  api: Api, project: DevInstanceProject, projectId: string, attached: Map<string, Tree>, field: Field,
): Promise<FieldDefault> {
  const host = attached.get(field.name)!;
  const value = host.bundle.values.find((v: Tree) => v.name === field.defaultValue);
  // Без `$type` у значения сервер отвечает 500 java.lang.InstantiationException
  const got: Tree = await api.post(`/api/admin/projects/${projectId}/customFields/${host.id}`,
    { defaultValues: [{ id: value.id, $type: value.$type }] }, "defaultValues(name)");
  const want = JSON.stringify([field.defaultValue]);
  const names = JSON.stringify((got.defaultValues ?? []).map((v: Tree) => v.name));
  if (names !== want) {
    fail("field_default_mismatch", `умолчание ${project.key} ${field.name} ${names} вместо ${want}`);
  }
  return { field: field.name, default: got.defaultValues[0].name, required: !host.canBeEmpty };
}

async function attachFields(
  api: Api, project: DevInstanceProject, projectId: string, prototypes: Map<string, string>,
  ownerIds: ReadonlyMap<string, string>,
): Promise<AttachedFields> {
  const attached = new Map<string, Tree>();
  const owners: ValueOwner[] = [];
  for (const field of project.fields) {
    const [got, owned] = await attach(api, project, projectId, prototypes, ownerIds, field);
    attached.set(field.name, got);
    owners.push(...owned);
  }
  for (const field of project.fields) {
    if (field.condition) await applyCondition(api, projectId, attached, field);
  }
  const defaults: FieldDefault[] = [];
  for (const field of project.fields.filter((f) => f.defaultValue !== undefined)) {
    defaults.push(await applyDefault(api, project, projectId, attached, field));
  }
  return { attached, defaults, owners };
}

async function requireWhenShown(
  api: Api, project: DevInstanceProject, projectId: string, attached: Map<string, Tree>,
): Promise<RequiredWhenShown[]> {
  const flagged: RequiredWhenShown[] = [];
  for (const field of project.fields.filter((f) => f.requiredWhenShown)) {
    const got: Tree = await api.post(`/api/admin/projects/${projectId}/customFields/${attached.get(field.name)!.id}`,
      { canBeEmpty: false }, "canBeEmpty,condition(field(field(name)),values(name))");
    const diverged = divergence({ canBeEmpty: false, ...field.condition! }, {
      canBeEmpty: got.canBeEmpty,
      field: got.condition?.field?.field?.name ?? null,
      value: (got.condition?.values ?? []).map((v: Tree) => v.name).join(", "),
    });
    if (diverged.length) {
      fail("required_condition_mismatch",
        `обязательность ${project.key} ${field.name} под условием расходится с записанной: ${diverged.join(", ")}`);
    }
    flagged.push({ field: field.name, when: field.condition! });
  }
  return flagged;
}

async function writeFieldOrder(
  api: Api, project: DevInstanceProject, projectId: string, attached: Map<string, Tree>,
): Promise<FieldOrder> {
  for (const [index, name] of project.order.entries()) {
    // С единицы: привязанное поле приходит с ordinal 0, и запись нуля сверка по ответу не отличила бы от пропущенной
    const ordinal = index + 1;
    const got: Tree = await api.post(`/api/admin/projects/${projectId}/customFields/${attached.get(name)!.id}`,
      { ordinal }, "ordinal");
    if (got.ordinal !== ordinal) {
      fail("field_order_mismatch", `ordinal ${project.key} ${name} ${JSON.stringify(got.ordinal)} вместо ${ordinal}`);
    }
  }
  return { project: project.key, fields: project.order };
}

/** Поля по умолчанию, удалённые из нового проекта, ломают использование до привязки полей из таблицы */
async function checkWorkflowUsages(api: Api, project: DevInstanceProject): Promise<void> {
  const catalog: Tree[] = await api.get(WORKFLOW_CATALOG, "title,usages(project(shortName),isBroken)");
  const broken = project.workflows.filter((title) => !catalog.some((w) => w.title === title &&
    (w.usages as Tree[]).some((usage) => usage.project?.shortName === project.key && usage.isBroken === false)))
    .map((title) => JSON.stringify(title));
  if (broken.length) {
    fail("workflow_broken", `воркфлоу не исполняются на ${project.key}: ${broken.join(", ")}`);
  }
}

/**
 * Проект, созданный через REST, получает весь глобальный каталог типов работ, и `autoAttached`
 * на это не влияет, поэтому набор пишется целиком — сервер принимает его и при выключенном учёте.
 */
async function writeTimeTracking(
  api: Api, project: DevInstanceProject, projectId: string, attached: Map<string, Tree>, types: Map<string, Tree>,
  want: TimeTracking,
): Promise<TimeTracking> {
  const field = (name: string | null): Tree | undefined =>
    name === null ? undefined : { id: attached.get(name)!.id, $type: attached.get(name)!.$type };
  const settings: Tree = await api.post(`/api/admin/projects/${projectId}/timeTrackingSettings`, {
    enabled: want.enabled,
    estimate: field(want.estimate),
    timeSpent: field(want.timeSpent),
    workItemTypes: want.workItemTypes.map((name) => ({ id: types.get(name)!.id })),
  }, "enabled,estimate(field(name)),timeSpent(field(name)),workItemTypes(name)");
  const diverged = divergence({ enabled: want.enabled, estimate: want.estimate, timeSpent: want.timeSpent }, {
    enabled: settings.enabled,
    estimate: settings.estimate?.field.name ?? null,
    timeSpent: settings.timeSpent?.field.name ?? null,
  });
  // Порядок в теле сервер не хранит и отдаёт типы в порядке глобального каталога: с таблицей этот порядок
  // сводит заведение недостающих типов в её порядке
  const got: string[] = (settings.workItemTypes ?? []).map((t: Tree) => t.name);
  if (JSON.stringify(got) !== JSON.stringify(want.workItemTypes)) {
    diverged.push(`типы работ ${JSON.stringify(got)} вместо ${JSON.stringify(want.workItemTypes)}`);
  }
  if (diverged.length) {
    fail("time_tracking_mismatch", `учёт времени ${project.key} расходится с полигоном: ${diverged.join(", ")}`);
  }
  return want;
}

/**
 * Атрибут работ глобален, как тип работ: заведённый раньше сверяется по значениям, а не заводится
 * заново, и к проекту привязывается уже он.
 */
async function ensureWorkItemAttribute(api: Api): Promise<string> {
  const path = "/api/admin/timeTrackingSettings/attributePrototypes";
  const fields = "id,name,values(name)";
  const want = WORK_ITEM_ATTRIBUTE;
  let prototype: Tree | undefined = (await api.get(path, fields)).find((a: Tree) => a.name === want.name);
  if (prototype === undefined) {
    prototype = await api.post(path, { name: want.name }, fields);
    for (const value of want.values) await api.post(`${path}/${prototype!.id}/values`, { name: value }, "id");
    prototype = await api.get(`${path}/${prototype!.id}`, fields);
  }
  const got: string[] = prototype!.values.map((v: Tree) => v.name);
  if (JSON.stringify(got) !== JSON.stringify(want.values)) {
    fail("work_item_attribute_mismatch",
      `атрибут работ ${JSON.stringify(want.name)} держит ${JSON.stringify(got)} вместо ${JSON.stringify(want.values)}`);
  }
  return prototype!.id;
}

async function attachWorkItemAttribute(api: Api, project: DevInstanceProject, projectId: string, prototypeId: string) {
  const attached: Tree = await api.post(`/api/admin/projects/${projectId}/timeTrackingSettings/attributes`,
    { prototype: { id: prototypeId } }, "name,values(name)");
  const got: string[] = attached.values.map((v: Tree) => v.name);
  if (attached.name !== WORK_ITEM_ATTRIBUTE.name || JSON.stringify(got) !== JSON.stringify(WORK_ITEM_ATTRIBUTE.values)) {
    fail("work_item_attribute_mismatch", `атрибут работ ${project.key} расходится с полигоном: ${attached.name} ${JSON.stringify(got)}`);
  }
}

type HubAccount = { id: string; token: string };

async function createHubUser(api: Api, user: DevInstanceUser, password: string, tokenName: string): Promise<HubAccount> {
  const created = await api.post("/hub/api/rest/users", {
    login: user.login, name: user.name, password,
    profile: { email: { email: user.email, verified: true } },
  }, "id");
  const { services } = await api.get("/hub/api/rest/services", "id,name");
  const youtrack = (services as Tree[]).find((s) => s.name === "YouTrack")!;
  const token = await api.post(`/hub/api/rest/users/${created.id}/permanenttokens`, {
    name: tokenName, scope: [{ id: youtrack.id }],
  }, "token");
  return { id: created.id, token: token.token };
}

function writeToken(file: string, token: string): void {
  writeFileSync(file, token + "\n");
  chmodSync(file, 0o600);
}

async function waitFor<T>(read: () => Promise<T>, ready: (got: T) => boolean, timedOut: (last: T) => never): Promise<T> {
  const deadline = Date.now() + POLL_TIMEOUT;
  for (;;) {
    const got = await read();
    if (ready(got)) return got;
    if (Date.now() > deadline) timedOut(got);
    await sleep(POLL_INTERVAL);
  }
}

/** Пользователь Hub становится пользователем YouTrack по запросу своим токеном, а до того `/api/users/{login}` — 404 */
async function awaitMemberEntity(member: Api): Promise<string> {
  const want = { login: MEMBER.login, email: MEMBER.email };
  const me: Tree = await waitFor(() => member.get("/api/users/me", "id,login,email"),
    (got) => divergence(want, got).length === 0,
    (got) => fail("member_mismatch", `участник расходится с таблицей: ${divergence(want, got).join(", ")}`));
  return me.id;
}

async function joinMemberGroup(api: Api, hubId: string): Promise<void> {
  const group = await api.post("/hub/api/rest/usergroups", { name: MEMBER.group }, "id");
  const added: Tree = await api.post(`/hub/api/rest/usergroups/${group.id}/users`, { id: hubId }, "login");
  if (added.login !== MEMBER.login) {
    fail("member_group_mismatch", `в группу ${JSON.stringify(MEMBER.group)} записан ` +
      `${JSON.stringify(added.login)} вместо ${JSON.stringify(MEMBER.login)}`);
  }
  // В `/api/groups` группа Hub появляется не сразу
  const listed = (groups: Tree[]): Tree[] => groups.filter((g) => g.name === MEMBER.group);
  await waitFor(() => api.get("/api/groups?$top=-1", "name,usersCount"),
    (groups) => listed(groups).some((g) => g.usersCount === 1),
    (groups) => fail("member_group_missing", `за ${POLL_TIMEOUT / 1000} с в /api/groups не появилась группа ` +
      `${JSON.stringify(MEMBER.group)} с одним пользователем: ${JSON.stringify(listed(groups))}`));
}

async function grantMemberRole(api: Api, member: Api, hubId: string, role: HubRole): Promise<void> {
  const granted: Tree = await api.post(`/hub/api/rest/users/${hubId}/projectroles`,
    { role: { id: role.role }, project: { id: role.project } }, "role(key),project(key)");
  const diverged = divergence({ role: MEMBER.role, project: HUB_PROJECT },
    { role: granted.role?.key ?? null, project: granted.project?.key ?? null });
  if (diverged.length) {
    fail("member_role_mismatch", `роль участника расходится с записанной: ${diverged.join(", ")}`);
  }
  // Роль доходит до токена участника через кэш прав секунд через пять, а до того DEV ему не виден
  const permissions = await waitFor(
    async (): Promise<string[]> => (await member.get("/api/permissions/cache?$top=-1", "permission(key)"))
      .map((cached: Tree) => cached.permission.key),
    (keys) => keys.includes(READ_ISSUE),
    (keys) => fail("member_role_not_applied",
      `за ${POLL_TIMEOUT / 1000} с в кэше прав участника не появилось ${READ_ISSUE}: ${keys.join(", ")}`));
  const excess = WITHHELD_PERMISSIONS.filter((key) => permissions.includes(key));
  if (excess.length) {
    fail("member_role_mismatch", `у участника права сверх роли ${MEMBER.role}: ${excess.join(", ")}`);
  }
}

async function setMemberTimeZone(api: Api, userId: string): Promise<void> {
  const profile: Tree = await api.post(`/api/users/${userId}/profiles/general`,
    { timezone: { id: MEMBER.timeZone } }, "timezone(id)");
  const got = profile.timezone?.id ?? null;
  if (got !== MEMBER.timeZone) {
    fail("profile_time_zone_mismatch",
      `пояс профиля участника ${JSON.stringify(got)} вместо ${JSON.stringify(MEMBER.timeZone)}`);
  }
}

async function createMember(api: Api, role: HubRole): Promise<[created: CreatedMember, member: Api]> {
  const account = await createHubUser(api, MEMBER, MEMBER_PASSWORD, "ytrack-dev-member");
  writeToken(MEMBER_TOKEN_FILE, account.token);
  const member = new Api(BASE_URL, account.token);
  const userId = await awaitMemberEntity(member);
  await joinMemberGroup(api, account.id);
  await grantMemberRole(api, member, account.id, role);
  await setMemberTimeZone(api, userId);
  return [{
    login: MEMBER.login, email: MEMBER.email, role: MEMBER.role, project: HUB_PROJECT, group: MEMBER.group,
    timeZone: MEMBER.timeZone,
  }, member];
}

/** Роль, выданная группе, в прямых ролях Hub не видна: её ловит список проектов, который видит токен dev.limited */
async function checkLimited(api: Api, limited: Pick<Api, "get">, hubId: string): Promise<CheckedLimited> {
  // Пустой список ролей Hub не отдаёт вовсе: ключа в ответе нет
  const user: Tree = await api.get(`/hub/api/rest/users/${hubId}`, "projectRoles(role(key),project(key))");
  const roles: string[] = (user.projectRoles ?? []).map((granted: Tree) => `${granted.role.key} ${granted.project.key}`);
  if (roles.length) {
    fail("limited_role_granted", `у ${LIMITED.login} есть роли Hub: ${roles.join(", ")}`);
  }
  const projects: string[] = (await limited.get("/api/admin/projects", "shortName")).map((p: Tree) => p.shortName);
  if (projects.length) {
    fail("limited_projects_visible", `токену ${LIMITED.login} видны проекты: ${projects.join(", ")}`);
  }
  return { login: LIMITED.login, roles, projects };
}

function issueField(project: DevInstanceProject, { field, value }: FieldValue): Tree {
  const typed = fieldOf(project, field)!;
  const key = VALUE_KEY[valueType(typed)];
  const wrap = (item: string | number): unknown => key === null ? item : { [key]: item };
  return {
    $type: ISSUE_FIELD_TYPE[typed.typeId], name: field, value: Array.isArray(value) ? value.map(wrap) : wrap(value),
  };
}

// Дата сверяется днём UTC: любой момент дня UTC сервер хранит полднем этого дня
function identity(field: Field, value: FieldValue["value"] | null): FieldValue["value"] | null {
  if (typeof value !== "number") return value;
  switch (valueType(field)) {
    case "date":
      return calendarDay(value, "UTC");
    case "date and time":
      return new Date(value).toISOString();
    default:
      return value;
  }
}

/** Сверенные значения задачи: записанные, умолчания незаписанных полей и `Assignee`, которого поставил каскад */
function checkValues(project: DevInstanceProject, got: Tree, written: FieldValue[]): FieldValue[] {
  const received = new Map<string, any>((got.customFields as Tree[]).map((f) => [f.name, f.value]));
  const read = (name: string): FieldValue["value"] | null => {
    const field = fieldOf(project, name)!;
    const key = VALUE_KEY[valueType(field)];
    const unwrap = (item: Tree | null): any => key === null || item === null ? item : item[key];
    const value = received.get(name) ?? null;
    return identity(field, Array.isArray(value) ? value.map(unwrap) : unwrap(value));
  };
  const differing = (values: FieldValue[]): string[] => values
    .filter(({ field, value }) => JSON.stringify(read(field)) !== JSON.stringify(value))
    .map(({ field, value }) => `${field} ${JSON.stringify(read(field))} вместо ${JSON.stringify(value)}`);
  const values = written.map(({ field, value }) => ({ field, value: identity(fieldOf(project, field)!, value)! }));
  const diverged = differing(values);
  if (diverged.length) {
    fail("field_value_mismatch", `значения полей ${got.idReadable} расходятся с записанными: ${diverged.join(", ")}`);
  }
  const defaults = project.fields
    .filter((f) => f.defaultValue !== undefined && !written.some((v) => v.field === f.name))
    .map((f): FieldValue => ({ field: f.name, value: f.defaultValue! }));
  const undefaulted = differing(defaults);
  if (undefaulted.length) {
    fail("field_default_mismatch", `${got.idReadable} расходится с умолчаниями ${project.key}: ${undefaulted.join(", ")}`);
  }
  values.push(...defaults);
  const { source, target } = SUBSYSTEM_CASCADE;
  const owner = cascadeOwner(project, written);
  if (owner !== undefined) {
    const assignee = read(target);
    if (assignee !== owner) {
      fail("subsystem_cascade_missing",
        `${target} у ${got.idReadable} ${JSON.stringify(assignee)} вместо владельца ${source} ${JSON.stringify(owner)}`);
    }
    values.push({ field: target, value: owner });
  }
  const order = (value: FieldValue): number => project.fields.findIndex((f) => f.name === value.field);
  return values.sort((a, b) => order(a) - order(b));
}

async function createIssue(
  api: Api, project: DevInstanceProject, projectId: string, issue: Pick<Issue, "summary" | "description">,
  written: FieldValue[],
): Promise<CreatedIssue> {
  const got: Tree = await api.post("/api/issues", {
    project: { id: projectId }, summary: issue.summary, description: issue.description,
    customFields: written.map((value) => issueField(project, value)),
  }, "id,idReadable,description,customFields(name,value(name,login,minutes,text))");
  const diverged = divergence({ description: issue.description ?? null }, got);
  if (diverged.length) {
    fail("description_mismatch", `описание ${got.idReadable} расходится с записанным: ${diverged.join(", ")}`);
  }
  return {
    id: got.id, idReadable: got.idReadable, description: issue.description ?? null,
    values: checkValues(project, got, written),
  };
}

/**
 * Слот ищется по фразе среди слотов задачи, а не собирается из id типа и суффикса:
 * форма его идентификатора в спеке не описана.
 */
async function createLinks(api: Api, issues: Map<Issue, CreatedIssue>): Promise<CreatedLink[]> {
  const created: CreatedLink[] = [];
  for (const [from, phrase, to] of LINKS) {
    const source = issues.get(from)!;
    const target = issues.get(to)!;
    const slots: Tree[] = await api.get(`/api/issues/${source.id}/links`,
      "id,direction,linkType(name,directed,sourceToTarget,targetToSource)");
    const matching = slots.filter((slot) =>
      (slot.direction === "INWARD" ? slot.linkType.targetToSource : slot.linkType.sourceToTarget) === phrase);
    if (matching.length !== 1) {
      fail("link_slot_missing", `у ${source.idReadable} слотов с фразой ${JSON.stringify(phrase)}: ${matching.length}`);
    }
    const type: Tree = matching[0].linkType;
    const added: Tree = await api.post(`/api/issues/${source.id}/links/${matching[0].id}/issues`,
      { id: target.id }, "idReadable,links(direction,linkType(name),issues(idReadable))");
    if (added.idReadable !== target.idReadable) {
      fail("link_mismatch", `связь ${source.idReadable} ${JSON.stringify(phrase)} вернула ` +
        `${JSON.stringify(added.idReadable)} вместо ${target.idReadable}`);
    }
    // Ответ — партнёр при любом слоте, поэтому направление сверяется по его обратной стороне и выводится из
    // фразы, а не из выбранного слота: при `sourceToTarget` партнёр — цель, и источник у него во входящем слоте
    const want = !type.directed ? "BOTH" : type.sourceToTarget === phrase ? "INWARD" : "OUTWARD";
    const got = (added.links as Tree[])
      .filter((slot) => slot.linkType.name === type.name &&
        slot.issues.some((issue: Tree) => issue.idReadable === source.idReadable))
      .map((slot) => slot.direction);
    if (got.length !== 1 || got[0] !== want) {
      fail("link_direction_mismatch", `связь ${source.idReadable} ${JSON.stringify(phrase)} ${target.idReadable}: ` +
        `у ${target.idReadable} задача ${source.idReadable} в слотах ${type.name} ${JSON.stringify(got)} ` +
        `вместо ${JSON.stringify([want])}`);
    }
    created.push({ from: source.idReadable, phrase, to: added.idReadable });
  }
  return created;
}

async function createWorkItem(
  api: Api, author: string, issue: CreatedIssue, types: Map<string, Tree>, entry: WorkItem, storedDate: number,
): Promise<CreatedWorkItem> {
  const item: Tree = await api.post(`/api/issues/${issue.id}/timeTracking/workItems`, {
    duration: { minutes: entry.minutes },
    type: { id: types.get(entry.type.name)!.id },
    text: entry.text,
    date: entry.date,
  }, "issue(idReadable),author(login),duration(minutes),type(name),text,date");
  const want: CreatedWorkItem = {
    issue: issue.idReadable, author, type: entry.type.name, minutes: entry.minutes, text: entry.text, date: storedDate,
  };
  const diverged = divergence(want, {
    issue: item.issue?.idReadable ?? null, author: item.author?.login ?? null, type: item.type?.name ?? null,
    minutes: item.duration?.minutes ?? null, text: item.text, date: item.date,
  });
  if (diverged.length) {
    fail("work_item_mismatch", `запись времени у ${issue.idReadable} расходится с записанной: ${diverged.join(", ")}`);
  }
  return want;
}

async function createAttachment(api: Api, issues: Map<Issue, CreatedIssue>): Promise<CreatedAttachment> {
  const issue = issues.get(ATTACHMENT.issue)!;
  const file = new Blob([ATTACHMENT.content]);
  const form = new FormData();
  form.append("files[0]", file, ATTACHMENT.name);
  const uploaded: Tree[] = await api.upload(`/api/issues/${issue.id}/attachments`, form,
    "issue(idReadable),name,size");
  const want: CreatedAttachment = { issue: issue.idReadable, name: ATTACHMENT.name, size: file.size };
  const diverged = uploaded.length === 1
    ? divergence(want, { issue: uploaded[0].issue?.idReadable ?? null, name: uploaded[0].name, size: uploaded[0].size })
    : [`вложений в ответе ${uploaded.length} вместо 1`];
  if (diverged.length) {
    fail("attachment_mismatch", `вложение у ${issue.idReadable} расходится с загруженным: ${diverged.join(", ")}`);
  }
  return want;
}

async function createComment(api: Api, author: string, issue: CreatedIssue, text: string): Promise<CreatedComment> {
  const comment: Tree = await api.post(`/api/issues/${issue.id}/comments`, { text },
    "id,issue(idReadable),author(login),text");
  const want = { issue: issue.idReadable, author, text };
  const diverged = divergence(want, {
    issue: comment.issue?.idReadable ?? null, author: comment.author?.login ?? null, text: comment.text,
  });
  if (diverged.length) {
    fail("comment_mismatch", `комментарий у ${issue.idReadable} расходится с записанным: ${diverged.join(", ")}`);
  }
  return { id: comment.id, ...want };
}

async function createArticles(api: Api, projectId: string): Promise<CreatedArticle[]> {
  const fields = "id,idReadable,parentArticle(idReadable)";
  const body = (article: Article): Tree =>
    ({ project: { id: projectId }, summary: article.summary, content: article.content });
  const parent: Tree = await api.post("/api/articles", body(ARTICLE_TREE.parent), fields);
  const child: Tree = await api.post("/api/articles",
    { ...body(ARTICLE_TREE.child), parentArticle: { id: parent.id } }, fields);
  const articles: CreatedArticle[] = [
    { idReadable: parent.idReadable, parent: null },
    { idReadable: child.idReadable, parent: parent.idReadable },
  ];
  const diverged = divergence(
    Object.fromEntries(articles.map((article) => [article.idReadable, article.parent])),
    Object.fromEntries([parent, child].map((got) => [got.idReadable, got.parentArticle?.idReadable ?? null])),
  );
  if (diverged.length) {
    fail("article_mismatch", `родители статей расходятся с записанными: ${diverged.join(", ")}`);
  }
  return articles;
}

async function editHistory(api: Api, issue: CreatedIssue): Promise<[edited: CreatedIssue, comments: CreatedComment[]]> {
  let edited = issue;
  const comments: CreatedComment[] = [];
  const tag: Tree = await api.post("/api/tags", { name: HISTORY.tag }, "id,name");
  if (tag.name !== HISTORY.tag) {
    fail("tag_mismatch", `заведён тег ${JSON.stringify(tag.name)} вместо ${JSON.stringify(HISTORY.tag)}`);
  }
  // Каждая правка — отдельный запрос: одно тело с заголовком и описанием даёт две записи истории с одним `timestamp`
  for (const edit of HISTORY.edits) {
    switch (edit.kind) {
      case "summary": {
        const got: Tree = await api.post(`/api/issues/${issue.id}`, { summary: edit.to }, "summary");
        const diverged = divergence({ summary: edit.to }, got);
        if (diverged.length) {
          fail("summary_mismatch", `заголовок ${issue.idReadable} расходится с записанным: ${diverged.join(", ")}`);
        }
        break;
      }
      case "description": {
        const got: Tree = await api.post(`/api/issues/${issue.id}`, { description: edit.to }, "description");
        const diverged = divergence({ description: edit.to }, got);
        if (diverged.length) {
          fail("description_mismatch", `описание ${issue.idReadable} расходится с записанным: ${diverged.join(", ")}`);
        }
        edited = { ...edited, description: got.description };
        break;
      }
      case "tag added": {
        const added: Tree = await api.post(`/api/issues/${issue.id}/tags`, { id: tag.id }, "name");
        if (added.name !== HISTORY.tag) {
          fail("tag_mismatch",
            `к ${issue.idReadable} навешен тег ${JSON.stringify(added.name)} вместо ${JSON.stringify(HISTORY.tag)}`);
        }
        break;
      }
      case "tag removed": {
        // Снятие отвечает пустым телом. Сам тег не удаляется: удалённый, он уносит из истории свои записи `TagsCategory`
        await api.delete(`/api/issues/${issue.id}/tags/${tag.id}`);
        const left: string[] = (await api.get(`/api/issues/${issue.id}`, "tags(name)")).tags.map((t: Tree) => t.name);
        if (left.length) {
          fail("tag_mismatch",
            `у ${issue.idReadable} после снятия ${JSON.stringify(HISTORY.tag)} остались теги ${JSON.stringify(left)}`);
        }
        break;
      }
      case "comment text": {
        const comment = await createComment(api, ADMIN_LOGIN, issue, edit.from);
        const edited: Tree = await api.post(`/api/issues/${issue.id}/comments/${comment.id}`, { text: edit.to }, "text");
        if (edited.text !== edit.to) {
          fail("comment_mismatch", `текст комментария у ${issue.idReadable} ${JSON.stringify(edited.text)} ` +
            `вместо ${JSON.stringify(edit.to)}`);
        }
        comments.push({ ...comment, text: edit.to });
        break;
      }
      default:
        unreachable(edit);
    }
  }
  return [edited, comments];
}

async function voteByComment(member: Api, issue: CreatedIssue): Promise<[comment: CreatedComment, vote: Vote]> {
  const comment = await createComment(member, MEMBER.login, issue, HISTORY.memberComment);
  const got: Tree = await member.get(`/api/issues/${issue.id}`, "votes,voters(hasVote)");
  const diverged = divergence({ votes: 1, hasVote: true }, { votes: got.votes, hasVote: got.voters?.hasVote ?? null });
  if (diverged.length) {
    fail("vote_mismatch", `голос ${MEMBER.login} за ${issue.idReadable} расходится с ожидаемым: ${diverged.join(", ")}`);
  }
  return [comment, { issue: issue.idReadable, voter: MEMBER.login }];
}

// Тег в истории называет имя, пользователя — логин, хотя имя у него тоже есть, комментарий и запись времени — текст
function named(value: unknown): unknown {
  return Array.isArray(value) ? value.map((item: Tree) => item.login ?? item.name ?? item.text) : value;
}

function editActivities(edit: HistoryEdit): Activity[] {
  switch (edit.kind) {
    case "summary":
      return [{ category: "SummaryCategory", removed: edit.from, added: edit.to }];
    case "description":
      return [{ category: "DescriptionCategory", removed: edit.from, added: edit.to }];
    case "tag added":
      return [{ category: "TagsCategory", added: [HISTORY.tag], removed: [] }];
    case "tag removed":
      return [{ category: "TagsCategory", added: [], removed: [HISTORY.tag] }];
    case "comment text":
      return [
        { category: "CommentsCategory", author: ADMIN_LOGIN },
        { category: "CommentTextCategory", removed: edit.from, added: edit.to },
      ];
  }
}

async function checkHistory(api: Api, issue: CreatedIssue): Promise<IssueHistory> {
  // Записи самого сидирования сверяются точным числом, а голос, который ставит воркфлоу, — включением
  const exact: Activity[] = [
    { category: "IssueCreatedCategory" },
    ...HISTORY.edits.flatMap(editActivities),
    { category: "CommentsCategory", author: MEMBER.login },
    { category: "WorkItemCategory", author: MEMBER.login },
  ];
  const present: Activity[] = [
    { category: "TotalVotesCategory", author: MEMBER.login, added: 1 },
    { category: "VotersCategory", added: [MEMBER.login] },
  ];
  const categories = [...new Set([...exact, ...present].map((activity) => activity.category))];
  const path = `/api/activities?issueQuery=${encodeURIComponent(`issue id: ${issue.idReadable}`)}` +
    `&categories=${categories.join(",")}&$top=100`;
  const records: Activity[] = (await api.get(path,
    "category(id),author(login),added($type,name,login,text),removed($type,name,login,text)"))
    .map((record: Tree): Activity => ({
      category: record.category.id, author: record.author?.login,
      added: named(record.added), removed: named(record.removed),
    }));
  const matches = (want: Activity, got: Activity): boolean => Object.entries(want)
    .every(([key, value]) => JSON.stringify(got[key as keyof Activity]) === JSON.stringify(value));
  const diverged: string[] = [];
  for (const category of new Set(exact.map((activity) => activity.category))) {
    const want = exact.filter((activity) => activity.category === category);
    const got = records.filter((activity) => activity.category === category);
    if (got.length !== want.length || !want.every((activity, i) => matches(activity, got[i]))) {
      diverged.push(`${JSON.stringify(got)} вместо ${JSON.stringify(want)}`);
    }
  }
  diverged.push(...present.filter((want) => !records.some((got) => matches(want, got)))
    .map((want) => `нет ${JSON.stringify(want)}`));
  if (diverged.length) {
    fail("history_mismatch", `история ${issue.idReadable} расходится с записанной: ${diverged.join("; ")}`);
  }
  return {
    issue: issue.idReadable,
    categories: Object.fromEntries(categories.map((category) =>
      [category, records.filter((activity) => activity.category === category).length])),
  };
}

async function rejectWorkItem(api: Api, project: DevInstanceProject, issue: CreatedIssue): Promise<number> {
  try {
    await api.post(`/api/issues/${issue.id}/timeTracking/workItems`, { duration: { minutes: 30 } });
  } catch (error) {
    if (error instanceof ApiError && error.status === 403) return error.status;
    throw error;
  }
  fail("time_tracking_not_disabled", `${issue.idReadable} приняла запись времени, хотя учёт ${project.key} выключен`);
}

async function main(): Promise<void> {
  checkReferences();
  checkFields();
  checkTextFeatures();
  checkTimeZoneFixture();
  checkHistoryTable();
  checkValueTable();
  checkFieldOrder();
  if (!existsSync(TOKEN_FILE)) fail("token_missing", `нет ${TOKEN_FILE}: сначала \`make wizard\``);
  const api = new Api(BASE_URL, readFileSync(TOKEN_FILE, "utf8").trim());

  await checkCoverage(api);
  const prototypes = await resolvePrototypes(api);
  await checkWorkflows(api);
  const memberRole = await resolveMemberRole(api);
  await checkInstanceTimeZone(api);
  const workItemTypes = await ensureWorkItemTypes(api);
  const workItemAttribute = await ensureWorkItemAttribute(api);
  await ensureLinkType(api);
  const linkTypes = await checkLinkCoverage(api);
  const admin: Tree = await api.get("/api/users/me", "id");
  const ownerIds = new Map<string, string>([[VALUE_OWNER, admin.id]]);
  const projectId = await createProject(api, DEV_PROJECT, admin.id);
  const dev = await attachFields(api, DEV_PROJECT, projectId, prototypes, ownerIds);
  const timeTracking = await writeTimeTracking(api, DEV_PROJECT, projectId, dev.attached, workItemTypes, {
    enabled: true, ...TIME_TRACKING, workItemTypes: WORK_ITEM_TYPES.filter((t) => !t.globalOnly).map((t) => t.name),
  });
  await attachWorkItemAttribute(api, DEV_PROJECT, projectId, workItemAttribute);
  await checkWorkflowUsages(api, DEV_PROJECT);

  const limitedAccount = await createHubUser(api, LIMITED, LIMITED_PASSWORD, "ytrack-dev-limited");
  writeToken(LIMITED_TOKEN_FILE, limitedAccount.token);
  const limited: Pick<Api, "get"> = new Api(BASE_URL, limitedAccount.token);
  const [member, memberApi] = await createMember(api, memberRole);
  const issues = new Map<Issue, CreatedIssue>();
  for (const issue of ISSUES) {
    issues.set(issue, await createIssue(api, DEV_PROJECT, projectId, issue, writtenValues(issue)));
  }
  const links = await createLinks(api, issues);
  // Полночь UTC в поясе admin — тот же день, и дата хранится как отправлена
  const workItem = await createWorkItem(api, ADMIN_LOGIN, issues.get(WORK_ITEM.issue)!, workItemTypes, WORK_ITEM,
    WORK_ITEM.date);
  const attachment = await createAttachment(api, issues);
  const comment = await createComment(api, ADMIN_LOGIN, issues.get(COMMENT.issue)!, COMMENT.text);
  const articles = await createArticles(api, projectId);
  const [historyIssue, historyComments] = await editHistory(api, issues.get(HISTORY.issue)!);
  const [memberComment, vote] = await voteByComment(memberApi, historyIssue);
  const memberWorkItem = await createWorkItem(memberApi, MEMBER.login, historyIssue, workItemTypes,
    HISTORY.memberWorkItem, HISTORY.memberWorkItem.storedDate);
  const history = await checkHistory(api, historyIssue);
  const devOrder = await writeFieldOrder(api, DEV_PROJECT, projectId, dev.attached);
  // Флаги — последние записи своего проекта: с флагом сервер не создаёт отклонённую задачу без причины, а DEV-2
  // заведена такой
  const devRequired = await requireWhenShown(api, DEV_PROJECT, projectId, dev.attached);
  const docsId = await createProject(api, DOCS_PROJECT, admin.id);
  const docs = await attachFields(api, DOCS_PROJECT, docsId, prototypes, ownerIds);
  const docsTimeTracking = await writeTimeTracking(api, DOCS_PROJECT, docsId, docs.attached, workItemTypes, {
    enabled: false, estimate: null, timeSpent: null, workItemTypes: DOCS.workItemTypes.map((t) => t.name),
  });
  await checkWorkflowUsages(api, DOCS_PROJECT);
  const docsIssue = await createIssue(api, DOCS_PROJECT, docsId, DOCS.issue, []);
  const docsWorkItemWrite = await rejectWorkItem(api, DOCS_PROJECT, docsIssue);
  const docsOrder = await writeFieldOrder(api, DOCS_PROJECT, docsId, docs.attached);
  const docsRequired = await requireWhenShown(api, DOCS_PROJECT, docsId, docs.attached);
  const limitedUser = await checkLimited(api, limited, limitedAccount.id);

  const typeCount = new Set(DEV_PROJECT.fields.map((f) => f.typeId)).size;
  console.log(`base_url: "${BASE_URL}"`);
  console.log(`project: "${DEV_PROJECT.key}"`);
  console.log(`project_id: "${projectId}"`);
  console.log(`fields: ${DEV_PROJECT.fields.length}`);
  console.log(`field_types: ${typeCount}`);
  console.log(`link_types: ${linkTypes}`);
  console.log(`admin_token_file: "${TOKEN_FILE}"`);
  console.log(`limited_token_file: "${LIMITED_TOKEN_FILE}"`);
  console.log(`member_token_file: "${MEMBER_TOKEN_FILE}"`);
  console.log("time_tracking:");
  console.log(`  enabled: ${timeTracking.enabled}`);
  console.log(`  estimate: ${JSON.stringify(timeTracking.estimate)}`);
  console.log(`  time_spent: ${JSON.stringify(timeTracking.timeSpent)}`);
  console.log("  work_item_types:");
  for (const name of timeTracking.workItemTypes) console.log(`    - ${JSON.stringify(name)}`);
  console.log("issues:");
  for (const issue of issues.values()) console.log(`  - ${JSON.stringify(issue.idReadable)}`);
  const edited = new Map(issues).set(HISTORY.issue, historyIssue);
  console.log("descriptions:");
  for (const issue of [...edited.values()].filter((i) => i.description !== null)) {
    console.log(`  - {issue: ${JSON.stringify(issue.idReadable)}, description: ${JSON.stringify(issue.description)}}`);
  }
  console.log("links:");
  for (const { from, phrase, to } of links) {
    console.log(`  - {from: ${JSON.stringify(from)}, phrase: ${JSON.stringify(phrase)}, to: ${JSON.stringify(to)}}`);
  }
  console.log("work_items:");
  for (const item of [workItem, memberWorkItem]) {
    console.log(`  - {issue: ${JSON.stringify(item.issue)}, author: ${JSON.stringify(item.author)}, ` +
      `type: ${JSON.stringify(item.type)}, minutes: ${item.minutes}, ` +
      `date: ${JSON.stringify(new Date(item.date).toISOString())}}`);
  }
  console.log("attachments:");
  console.log(`  - {issue: ${JSON.stringify(attachment.issue)}, name: ${JSON.stringify(attachment.name)}, ` +
    `size: ${attachment.size}}`);
  console.log("comments:");
  for (const { issue, author, text } of [comment, ...historyComments, memberComment]) {
    console.log(`  - {issue: ${JSON.stringify(issue)}, author: ${JSON.stringify(author)}, text: ${JSON.stringify(text)}}`);
  }
  console.log("articles:");
  for (const article of articles) {
    console.log(`  - {article: ${JSON.stringify(article.idReadable)}, parent: ${JSON.stringify(article.parent)}}`);
  }
  const attachedProjects = [[DEV_PROJECT, dev], [DOCS_PROJECT, docs]] as const;
  console.log("field_defaults:");
  for (const [project, { defaults }] of attachedProjects) {
    for (const { field, default: value, required } of defaults) {
      console.log(`  - {project: ${JSON.stringify(project.key)}, field: ${JSON.stringify(field)}, ` +
        `default: ${JSON.stringify(value)}, required: ${required}}`);
    }
  }
  console.log("required_when_shown:");
  for (const [project, flagged] of [[DEV_PROJECT, devRequired], [DOCS_PROJECT, docsRequired]] as const) {
    for (const { field, when } of flagged) {
      console.log(`  - {project: ${JSON.stringify(project.key)}, field: ${JSON.stringify(field)}, ` +
        `when: {field: ${JSON.stringify(when.field)}, value: ${JSON.stringify(when.value)}}}`);
    }
  }
  console.log("subsystem_owners:");
  for (const [project, { owners }] of attachedProjects) {
    for (const { field, value, owner } of owners) {
      console.log(`  - {project: ${JSON.stringify(project.key)}, field: ${JSON.stringify(field)}, ` +
        `value: ${JSON.stringify(value)}, owner: ${JSON.stringify(owner)}}`);
    }
  }
  console.log("users:");
  console.log(`  - {login: ${JSON.stringify(limitedUser.login)}, roles: ${JSON.stringify(limitedUser.roles)}, ` +
    `projects: ${JSON.stringify(limitedUser.projects)}}`);
  console.log(`  - {login: ${JSON.stringify(member.login)}, email: ${JSON.stringify(member.email)}, ` +
    `role: ${JSON.stringify(member.role)}, project: ${JSON.stringify(member.project)}, ` +
    `group: ${JSON.stringify(member.group)}, time_zone: ${JSON.stringify(member.timeZone)}}`);
  console.log("history:");
  console.log(`  issue: ${JSON.stringify(history.issue)}`);
  console.log("  categories:");
  for (const [category, count] of Object.entries(history.categories)) console.log(`    ${category}: ${count}`);
  console.log("votes:");
  console.log(`  - {issue: ${JSON.stringify(vote.issue)}, voter: ${JSON.stringify(vote.voter)}}`);
  const valued = issues.get(ALL_TYPES_ISSUE)!;
  const valueOf = (field: string): string => JSON.stringify(valued.values.find((v) => v.field === field)!.value);
  const valuedTypes = new Set(valued.values.filter((v) => !isEmpty(v.value))
    .map((v) => fieldOf(DEV_PROJECT, v.field)!.typeId));
  console.log("values:");
  console.log(`  issue: ${JSON.stringify(valued.idReadable)}`);
  console.log(`  field_types: ${valuedTypes.size}`);
  console.log("  fields:");
  for (const { field, value } of valued.values) {
    console.log(`    - {field: ${JSON.stringify(field)}, value: ${JSON.stringify(value)}}`);
  }
  console.log("subsystem_cascade:");
  console.log(`  issue: ${JSON.stringify(valued.idReadable)}`);
  console.log(`  subsystem: ${valueOf(SUBSYSTEM_CASCADE.source)}`);
  console.log(`  assignee: ${valueOf(SUBSYSTEM_CASCADE.target)}`);
  console.log("time_tracking_disabled_project:");
  console.log(`  project: ${JSON.stringify(DOCS_PROJECT.key)}`);
  console.log(`  fields: ${DOCS_PROJECT.fields.length}`);
  console.log(`  enabled: ${docsTimeTracking.enabled}`);
  console.log("  work_item_types:");
  for (const name of docsTimeTracking.workItemTypes) console.log(`    - ${JSON.stringify(name)}`);
  console.log("  issues:");
  console.log(`    - ${JSON.stringify(docsIssue.idReadable)}`);
  console.log(`  work_item_write: ${docsWorkItemWrite}`);
  console.log("field_order:");
  for (const { project, fields } of [devOrder, docsOrder]) {
    console.log(`  - project: ${JSON.stringify(project)}`);
    console.log("    fields:");
    for (const field of fields) console.log(`      - ${JSON.stringify(field)}`);
  }
}

main().catch((error) => {
  if (error instanceof ApiError) fail("api_error", error.message);
  throw error;
});
