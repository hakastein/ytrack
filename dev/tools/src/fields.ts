/**
 * Поля проектов полигона: DEV несёт все 20 типов кастом-полей, DOCS — набор проекта с выключенным учётом времени.
 *
 * `Нужен реквеcт на выпуск` пишется через латинскую `c`: значение, которое читается как
 * написанное кириллицей, но сервером не находится.
 */

export type BundleValue = {
  name: string;
  isResolved?: boolean;
  archived?: boolean;
  released?: boolean;
  /** Логин владельца: воркфлоу Subsystem Assignee ставит его в пустой `Assignee` задачи с этим значением */
  owner?: string;
};

/** Строки каталога `/api/admin/customFieldSettings/types`: по ним tsc требует строку в каждой таблице по типу поля */
export type TypeId =
  | "enum[1]" | "enum[*]" | "state[1]" | "version[1]" | "version[*]" | "build[1]" | "build[*]" | "ownedField[1]"
  | "ownedField[*]" | "user[1]" | "user[*]" | "group[1]" | "group[*]" | "date" | "date and time" | "integer" | "float"
  | "string" | "text" | "period";

type ValueTypeOf<T extends string> = T extends `${infer V}[${string}]` ? V : T;

export type ValueType = ValueTypeOf<TypeId>;

export type Field = {
  name: string;
  typeId: TypeId;
  values?: BundleValue[];
  required?: boolean;
  /** Тот же `canBeEmpty: false`, но под `condition` и выставленный после задач полигона, а не при привязке */
  requiredWhenShown?: boolean;
  defaultValue?: string;
  /** Прототип уже заведён инстансом и несёт `localizedName`, которого своему не досталось */
  predefined?: boolean;
  /** Поле появляется на задаче, только когда названное поле принимает названное значение */
  condition?: { field: string; value: string };
};

export const valueType = (field: Field): ValueType => field.typeId.split("[")[0] as ValueType;

const enumValues = (...names: string[]): BundleValue[] => names.map((name) => ({ name }));

const stateValues = (...pairs: [string, boolean][]): BundleValue[] =>
  pairs.map(([name, isResolved]) => ({ name, isResolved }));

export const FIELDS: Field[] = [
  {
    name: "State", typeId: "state[1]", predefined: true, defaultValue: "Новая", values: stateValues(
      ["In Progress", false], ["Done", false], ["Duplicate", true], ["Новая", false],
      ["Отклонена", true], ["Закрыта", true], ["Готова к работе", false],
      ["На проверке", false], ["На приёмке", false], ["Уточнение", false],
      ["Отложено", false]),
  },
  {
    name: "Type", typeId: "enum[1]", required: true, predefined: true,
    values: enumValues("Bug", "Epic", "User Story", "Task", "Инцидент"),
  },
  {
    name: "Priority", typeId: "enum[1]", required: true, predefined: true, defaultValue: "Low",
    values: enumValues("Critical", "Blocked", "Medium", "Low"),
  },
  { name: "Assignee", typeId: "user[1]", predefined: true },
  {
    name: "Категория", typeId: "enum[1]", required: true, values: enumValues(
      "Продукт", "Сопровождение", "Развитие технологий", "Global", "Внедрение", "Разработчики"),
  },
  {
    name: "Клиент", typeId: "enum[*]", required: true, values: enumValues(
      "ACME", "АЛЬФА", "Бета-Девелопмент", "ГАММА", 'ООО "РОМАШКА"', "NORTH WIND"),
  },
  {
    name: "Модуль системы", typeId: "enum[*]", required: true, values: enumValues(
      "Инфраструктура. DevOps", "Бэкенд. API", "Бэкенд. Очереди",
      "Фронтенд. Веб", "Фронтенд. Мобильный"),
  },
  {
    name: "Система", typeId: "enum[*]",
    values: enumValues("Бэкенд", "Фронтенд", "Сайт", "Инфраструктура", "Платформа"),
  },
  {
    name: "Статус разработки", typeId: "state[1]", values: stateValues(
      ["Передано в разработку", false], ["В разработке", false], ["Ревью", false],
      ["Готово к слиянию", false], ["Тестирование ветки", false], ["Готова к выпуску", false],
      ["Приёмочные тесты", false], ["На уточнении", false], ["Готова", true],
      ["Отложена", false], ["Требуются доработки", false], ["Нужен реквеcт на выпуск", false],
      ["Выпущена", true], ["Нужен MR в релиз", false], ["Готова к релизу", false],
      ["Релизные тесты", false], ["Провалена", true], ["Deploy", false]),
  },
  {
    name: "Статус анализа", typeId: "state[1]", values: stateValues(
      ["Подготовка решения", false], ["Подготовка ТЗ", false], ["Оценка решения", false],
      ["Решение согласовано", false], ["Готово к разработке", false],
      ["На рассмотрении", false], ["Готово к передаче", true],
      ["Отклонено", true], ["Реализовано", true], ["Бэклог спринта", false]),
  },
  {
    name: "Причина отклонения", typeId: "enum[1]", requiredWhenShown: true,
    condition: { field: "State", value: "Отклонена" },
    values: enumValues("Дубль", "Не воспроизводится", "Не наш модуль", "Передумали"),
  },
  {
    // Архивных больше, чем живых, как у всякого поля спринтов с историей
    name: "Плановый спринт", typeId: "version[*]", values: [
      { name: "SPR-78", archived: true }, { name: "SPR-24", archived: true },
      { name: "SPR-51", archived: true }, { name: "SPR-52", archived: true },
      { name: "SPR-92", archived: false }, { name: "SPR-93", archived: false },
    ],
  },
  {
    name: "Релиз", typeId: "version[1]", values: [
      { name: "2026.1", released: true }, { name: "2026.2", released: false },
    ],
  },
  {
    name: "Subsystem", typeId: "ownedField[1]", predefined: true,
    values: [{ name: "Ядро", owner: "admin" }, { name: "Отчёты" }, { name: "Интеграции" }],
  },
  { name: "Подсистемы", typeId: "ownedField[*]", values: enumValues("Биллинг", "Уведомления") },
  { name: "Fixed in build", typeId: "build[1]", predefined: true, values: enumValues("13757", "13874") },
  { name: "Сборки", typeId: "build[*]", values: enumValues("2026.1.1", "2026.1.2") },
  { name: "Соисполнители", typeId: "user[*]" },
  { name: "Группа доступа", typeId: "group[1]" },
  { name: "Группы доступа", typeId: "group[*]" },
  { name: "Плановая дата решения", typeId: "date" },
  { name: "Дата начала работы", typeId: "date and time" },
  { name: "Порядок реализации", typeId: "integer" },
  { name: "Коэффициент", typeId: "float" },
  { name: "Внешний номер", typeId: "string" },
  { name: "Примечание", typeId: "text" },
  // Прототипы заводит сам YouTrack под учёт времени, и вторых с этими именами не будет
  { name: "Оценка", typeId: "period", predefined: true },
  { name: "Затраченное время", typeId: "period", predefined: true },
];

export const DOCS_FIELDS: Field[] = [
  {
    name: "Priority", typeId: "enum[1]", required: true, predefined: true, defaultValue: "Low",
    values: enumValues("Critical", "Blocked", "Medium", "Low"),
  },
  {
    name: "State", typeId: "state[1]", required: true, predefined: true, defaultValue: "To do",
    values: stateValues(["To do", false], ["In Progress", false], ["Done", true]),
  },
  { name: "Assignee", typeId: "user[1]", predefined: true },
  { name: "Due Date", typeId: "date", predefined: true },
];

// Порядок DEV по `ordinal`
export const FIELD_ORDER: string[] = [
  "Type", "Priority", "Категория", "Клиент", "Модуль системы", "Система", "Assignee", "State", "Причина отклонения",
  "Соисполнители", "Статус анализа", "Плановый спринт", "Порядок реализации", "Плановая дата решения", "Релиз",
  "Статус разработки", "Затраченное время", "Оценка", "Дата начала работы", "Внешний номер",
  // Поля, добранные ради недостающих типов, идут в порядке FIELDS
  "Subsystem", "Подсистемы", "Fixed in build", "Сборки", "Группа доступа", "Группы доступа", "Коэффициент",
  "Примечание",
];

export const DOCS_FIELD_ORDER: string[] = ["State", "Priority", "Assignee", "Due Date"];
