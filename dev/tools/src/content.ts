export type TextFeature = [name: string, pattern: RegExp];

export type FieldValue = { field: string; value: string | number | string[] };

export type Issue = {
  summary: string;
  state: string;
  description?: string;
  textFeatures?: TextFeature[];
  values?: FieldValue[];
};

export const COMMON_VALUES: FieldValue[] = [
  { field: "Type", value: "Task" },
  { field: "Priority", value: "Medium" },
  { field: "Категория", value: "Развитие технологий" },
  { field: "Клиент", value: ["ACME"] },
  { field: "Модуль системы", value: ["Инфраструктура. DevOps"] },
];

const trailingSpaces = "Строка с хвостовыми пробелами   ";
const dashes = "---";
const tildes = "~~~";
const outsideBmp = "Символ вне BMP: \u{1F600}";

const emptyFirstLine = "\nОписание после пустой первой строки";
const leadingSpace = " Описание с ведущим пробелом\nи вторая строка без него";
const emptyThenLeadingSpace = "\n Описание с ведущим пробелом после пустой первой строки";

const inProgress: Issue = {
  summary: "Задача в работе", state: "In Progress",
  description: ["Описание с враждебной прозой.", trailingSpaces, dashes, tildes, outsideBmp].join("\n"),
  textFeatures: [
    ["строка с хвостовыми пробелами посреди текста", / +\n/],
    ["строка ровно ---", /^---$/m],
    ["строка ровно ~~~", /^~~~$/m],
    ["символ вне BMP", /[\u{10000}-\u{10FFFF}]/u],
    ["без перевода строки в конце", /[^\n]$/],
  ],
};
const rejected: Issue = { summary: "Отклонённая задача", state: "Отклонена" };
const blocker: Issue = {
  summary: "Блокирующая задача", state: "Новая", description: emptyFirstLine,
  textFeatures: [["пустая первая строка", /^\n/]],
};
const parent: Issue = {
  summary: "Родительская задача", state: "Новая", description: leadingSpace,
  textFeatures: [["ведущий пробел в первой строке", /^ /]],
};
const duplicate: Issue = {
  summary: "Дубль задачи в работе", state: "Новая", description: emptyThenLeadingSpace,
  textFeatures: [["пустая первая строка, за ней ведущий пробел", /^\n /]],
};
const copy: Issue = { summary: "Копия задачи в работе", state: "Новая" };

const historySummary = { from: "Задача с историей правок: заголовок до правки", to: "Задача с историей правок" };
const historyDescription = { from: "Описание задачи с историей до правки.", to: "Описание задачи с историей после правки." };
const withHistory: Issue = { summary: historySummary.from, state: "Новая", description: historyDescription.from };

const withValues: Issue = {
  summary: "Задача со значениями всех типов полей", state: "Новая",
  values: [
    { field: "Плановый спринт", value: ["SPR-92"] },
    { field: "Релиз", value: "2026.1" },
    { field: "Subsystem", value: "Ядро" },
    { field: "Подсистемы", value: ["Биллинг"] },
    { field: "Fixed in build", value: "13757" },
    { field: "Сборки", value: ["2026.1.1"] },
    { field: "Соисполнители", value: ["dev.member"] },
    { field: "Группа доступа", value: "DEVELOPMENT Team" },
    { field: "Группы доступа", value: ["Участники полигона"] },
    { field: "Плановая дата решения", value: Date.UTC(2026, 8, 16, 12) },
    { field: "Дата начала работы", value: Date.UTC(2026, 7, 31, 0, 0, 0, 123) },
    { field: "Порядок реализации", value: 42 },
    { field: "Коэффициент", value: 1.5 },
    { field: "Внешний номер", value: "EXT-1" },
    { field: "Примечание", value: "первая\nвторая" },
    { field: "Оценка", value: 90 },
  ],
};

export const ISSUES: Issue[] = [inProgress, rejected, blocker, parent, duplicate, copy, withHistory, withValues];

export const ALL_TYPES_ISSUE = withValues;

export const COPY_LINK = {
  name: "Copy",
  localizedName: null,
  sourceToTarget: "Скопирована в",
  localizedSourceToTarget: null,
  targetToSource: "Копия",
  localizedTargetToSource: "",
  directed: true,
  aggregation: true,
  readOnly: false,
};

export type Link = [from: Issue, phrase: string, to: Issue];

export const LINKS: Link[] = [
  [inProgress, "relates to", rejected],
  [inProgress, "depends on", blocker],
  [inProgress, "subtask of", parent],
  [inProgress, "is duplicated by", duplicate],
  [inProgress, "Скопирована в", copy],
];

export type WorkItemType = {
  name: string;
  autoAttached: boolean;
  predefined?: boolean;
  globalOnly?: boolean;
};

const development: WorkItemType = { name: "Разработка", autoAttached: true, predefined: true };
const aiDevelopment: WorkItemType = { name: "ИИРазработка", autoAttached: false, globalOnly: true };

export const WORK_ITEM_TYPES: WorkItemType[] = [
  development,
  { name: "Тестирование", autoAttached: true, predefined: true },
  { name: "Документирование", autoAttached: true, predefined: true },
  { name: "Исследование", autoAttached: false, predefined: true },
  { name: "Груминг", autoAttached: false },
  { name: "Декомпозиция", autoAttached: false },
  { name: "Кодревью", autoAttached: false },
  { name: "ПланированиеРетро", autoAttached: false },
  { name: "ТехОкружение", autoAttached: false },
  { name: "Коммуникации", autoAttached: false },
  { name: "Дизайн/Прототипирование", autoAttached: false },
  { name: "Написание ТЗ", autoAttached: false },
  { name: "Написание инструкции", autoAttached: false },
  { name: "Проектирование", autoAttached: false },
  { name: "Уточнение требований", autoAttached: false },
  aiDevelopment,
  { name: "Реализация", autoAttached: false, predefined: true, globalOnly: true },
];

export const TIME_TRACKING = { estimate: "Оценка", timeSpent: "Затраченное время" };

export const WORK_ITEM_ATTRIBUTE = { name: "Формат работы", values: ["Сам", "ИИагент"] };

export const DOCS = {
  workItemTypes: [...WORK_ITEM_TYPES.filter((t) => !t.globalOnly), aiDevelopment],
  issue: { summary: "Задача в проекте с выключенным учётом времени" },
};

export type WorkItem = { type: WorkItemType; minutes: number; text: string; date: number };

export const WORK_ITEM = {
  issue: inProgress,
  type: development,
  minutes: 90,
  text: "Разбор полигона",
  date: Date.UTC(2026, 8, 1),
};

export const ATTACHMENT = {
  issue: inProgress,
  name: "заметка-полигона.txt",
  content: "Вложение полигона: небольшой текст в UTF-8.\n",
};

export const COMMENT = {
  issue: inProgress,
  text: "Комментарий полигона: у задачи есть вложение и запись времени.",
};

const TEXT_EDIT_KINDS = ["summary", "description", "comment text"] as const;
const TAG_EDIT_KINDS = ["tag added", "tag removed"] as const;

export const HISTORY_EDIT_KINDS = [...TEXT_EDIT_KINDS, ...TAG_EDIT_KINDS];

export type HistoryEdit =
  | { kind: (typeof TEXT_EDIT_KINDS)[number]; from: string; to: string }
  | { kind: (typeof TAG_EDIT_KINDS)[number] };

export type History = {
  issue: Issue;
  tag: string;
  edits: HistoryEdit[];
  memberComment: string;
  memberWorkItem: WorkItem & { storedDate: number };
};

export const HISTORY: History = {
  issue: withHistory,
  tag: "история-полигона",
  edits: [
    { kind: "summary", ...historySummary },
    { kind: "description", ...historyDescription },
    { kind: "tag added" },
    { kind: "tag removed" },
    { kind: "comment text", from: "Комментарий администратора до правки.", to: "Комментарий администратора после правки." },
  ],
  memberComment: "Комментарий участника: +1",
  memberWorkItem: {
    type: development, minutes: 30, text: "Разбор истории правок",
    date: Date.UTC(2026, 8, 1, 15), storedDate: Date.UTC(2026, 8, 2),
  },
};

export type Article = { summary: string; content: string };

export const ARTICLE_TREE: { parent: Article; child: Article } = {
  parent: { summary: "Родительская статья", content: "Статья, в которую вложена дочерняя." },
  child: { summary: "Дочерняя статья", content: "Статья, вложенная в родительскую." },
};
