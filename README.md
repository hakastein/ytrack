# ytrack

Консольный клиент YouTrack, рассчитанный прежде всего на AI-агентов: компактный
машиночитаемый вывод, ошибки с кодами и команды, которые складываются пайпами. Свою
процедуру из нескольких шагов проект записывает командой на JavaScript, и агент
вызывает её одним `ytrack <имя>`.

- Один бинарник на Go, без зависимостей и без MCP-сервера.
- Весь вывод — YAML, который разбирается `yq`.
- Ошибка — тоже YAML-документ с машинным кодом, в stderr.
- Кастом-поля любых типов, включая множественные enum'ы, читаются и пишутся корректно.
- Свои команды — скрипты в `.ytrack/scripts` репозитория над API YouTrack из самого
  `ytrack`: без Node, без внешних программ, с той же справкой, выводом и ошибками, что
  у встроенных. Встроенные команды написаны так же.

```bash
ytrack issue list --query "project: DEV State: Open" --limit 20
ytrack issue show DEV-123 --comments 3
ytrack issue update DEV-123 --field State=Done
```

## Установка

Релиз выходит на каждый push в `main`. Версия календарная:
`v<ГГ>.<М>.<Д>.<номер запуска>`. Все релизы лежат
[на странице релизов](https://github.com/hakastein/ytrack/releases).
Последний бинарник для своей платформы скачивается по постоянной ссылке:

| Платформа | Бинарник |
|---|---|
| Linux amd64 | [ytrack-linux-amd64](https://github.com/hakastein/ytrack/releases/latest/download/ytrack-linux-amd64) |
| Linux arm64 | [ytrack-linux-arm64](https://github.com/hakastein/ytrack/releases/latest/download/ytrack-linux-arm64) |
| macOS arm64 | [ytrack-darwin-arm64](https://github.com/hakastein/ytrack/releases/latest/download/ytrack-darwin-arm64) |
| macOS amd64 | [ytrack-darwin-amd64](https://github.com/hakastein/ytrack/releases/latest/download/ytrack-darwin-amd64) |
| Windows amd64 | [ytrack-windows-amd64.exe](https://github.com/hakastein/ytrack/releases/latest/download/ytrack-windows-amd64.exe) |

Или из терминала через `gh`. Без указания тега он берёт последний релиз:

```bash
gh release download -R hakastein/ytrack -p ytrack-linux-amd64 -D ~/.local/bin
mv ~/.local/bin/ytrack-linux-amd64 ~/.local/bin/ytrack && chmod +x ~/.local/bin/ytrack
```

Контрольные суммы лежат в том же релизе, в файле `SHA256SUMS`. На macOS с бинарника,
скачанного браузером, нужно снять карантин: `xattr -d com.apple.quarantine ytrack`.

Собрать из исходников: `make build` кладёт бинарник в `bin/ytrack`.

### Скилл для агента

Скилл [`youtrack`](skills/youtrack/SKILL.md) говорит агенту в любом репозитории, что
с YouTrack работают через `ytrack`. Формат — [Agent Skills](https://agentskills.io),
его читают Claude Code, Codex, OpenCode, Gemini CLI, Copilot и Cursor.

```bash
npx skills add hakastein/ytrack -g -y
```

Без Node скопируйте `skills/youtrack/SKILL.md` в `~/.claude/skills/youtrack/` (Claude
Code) и в `~/.agents/skills/youtrack/` (остальные агенты).

## Авторизация

Нужны адрес инстанса и постоянный токен YouTrack. Передать их можно двумя способами.

**Переменные окружения** — удобно для агентов и CI:

```bash
export YTRACK_URL=https://youtrack.example.com
export YTRACK_TOKEN=perm:...
```

**Сохранённый вход** — удобно человеку:

```bash
ytrack auth login    # спросит адрес и токен и сохранит вход
ytrack auth status   # покажет адрес, источник входа и чей он токен
ytrack auth logout
```

Вход привязывается к каталогу, из которого вызван `login`: в разных проектах могут
быть разные инстансы и токены, а глобальный вход работает там, где своего нет.

Адрес и токен всегда берутся вместе из одного источника. Заданы обе переменные —
работает окружение, и файл входов даже не читается; не задана ни одна — работает
сохранённый вход. Одна переменная без второй отвергается с `bad_usage`: иначе
`YTRACK_URL=...` перед командой молча ушёл бы на адрес записи с её токеном. Чтобы
сходить на другой инстанс разово, передайте обе переменные. Сам токен `ytrack` не
печатает никогда.

`auth login` читает токен с терминала, поэтому агенту вход настраивает человек,
либо агент получает переменные окружения.

## Команды

Все команды имеют вид `ytrack <команда> <подкоманда>`:

| Команда | Подкоманды |
|---|---|
| `issue` | `list`, `show`, `create`, `update`, `delete` |
| `activity` | `list` |
| `comment` | `list`, `create`, `update`, `delete` |
| `link` | `list`, `add`, `remove` |
| `tag` | `list`, `create`, `delete`, `add`, `remove` |
| `field` | `list`, `show` |
| `time` | `list`, `create`, `update`, `delete` |
| `attachment` | `list`, `create`, `delete` |
| `article` | `list`, `show`, `create`, `update`, `delete` |
| `project` | `list`, `show` |
| `user` | `list`, `show` |
| `auth` | `login`, `status`, `logout` |

Подробное описание каждой команды — в `ytrack <команда> <подкоманда> --help`. Команды
проекта и пользователя (см. [Свои команды](#свои-команды)) `ytrack --help` показывает
отдельным блоком с каталогом, откуда они взяты.

Примеры:

```bash
# Создать задачу
ytrack issue create DEV --summary "Упал импорт" --field Type=Bug --field Priority=Major

# Прокомментировать, повесить тег, связать с другой задачей
ytrack comment create DEV-123 --text "Воспроизвёл на staging"
ytrack tag add DEV-123 --name triage
ytrack link add DEV-123 "depends on" DEV-100

# Выбрать нужные поля: --fields принимает выражение полей YouTrack,
# а с плюсом добавляет поля к набору по умолчанию
ytrack issue show DEV-123 --fields 'customFields(State,"Due Date")'
ytrack issue list --query "#Unresolved" --fields '+customFields(Priority)'

# Коммиты задачи: ссылка на каждый и, с плюсом, его сообщение
ytrack activity list DEV-123 --category VcsChangeCategory --fields '+added(text)'

# Разобрать вывод yq
ytrack issue list --query "project: DEV #Unresolved" | yq '.issues[].idReadable'
```

## Вывод

Каждая команда печатает в stdout один YAML-документ. `show` выводит объект
целиком, а длинный текст — литеральным блоком, байт в байт. `list` выводит общее число
найденного, признак обрезки по `--limit` и по записи на строку. Следующую страницу
даёт `--skip`: `--limit 50 --skip 50`.

Исключения — `ytrack completion`, который печатает скрипт для оболочки, и ответы
на запросы автодополнения.

## Ошибки

Ошибка печатается в stderr тоже YAML-документом. Первое поле — машинный код,
за ним человеческое сообщение и подробности:

```yaml
code: "denied"
message: "no login was found in the places under looked_in"
looked_in:
  - "YTRACK_URL"
  - "YTRACK_TOKEN"
  - "settings"
```

Коды: `bad_usage`, `unknown_name`, `missing_required`, `not_found`, `denied`,
`rejected`, `upstream_failed`, `upstream_invalid`, `write_uncertain`.

Коды завершения: `0` — успех, `1` — ошибка, `2` — инстанс мог измениться:
`write_uncertain` (запись ушла на сервер, но неизвестно, применилась ли она) или ошибка
после записи, например в своей команде, которая успела что-то записать. Повторять ли
запрос, решает вызывающий, сам `ytrack` ничего не ретраит.

`script_failed` — дефект своей команды проекта или пользователя: ошибка несёт `file`,
`line` и `column`, и чинить нужно скрипт, а не вызов. Встроенные команды его не дают.

## Свои команды

Процедура, которую агент повторяет из раза в раз, записывается командой: файл на
JavaScript, путь которого задаёт имя команды.

| Где лежит | Кому доступна |
|---|---|
| `.ytrack/scripts` в ближайшем каталоге над текущим | всем в этом репозитории |
| `~/.ytrack/scripts` | вам во всех проектах |

`.ytrack/scripts/triage.js` — это `ytrack triage`, `.ytrack/scripts/docs/map.js` —
`ytrack docs map`. Слова встроенных команд (`issue`, `tag`, …) заняты: заменить
`issue show` или добавить `issue close` нельзя.

```js
// .ytrack/scripts/triage.js
const { issues, comments, users } = require("ytrack/v1");

exports.definition = {
  short: "Take an issue into work",
  long: "Assign an issue to you, move it to In Progress " + "and leave a comment when --note is given.",
  args: [{ name: "id", type: "string", usage: "readable id of the issue, such as DEV-1" }],
  flags: [
    { name: "note", type: "string", usage: "comment `text`" },
    { name: "fields", type: "fields", default: "idReadable,summary,customFields(State,Assignee)" },
  ],
};

exports.command = (id, flags) => {
  const me = users.me();
  issues.update({ id, customFields: { Assignee: me.login, State: "In Progress" }, fields: "idReadable" });
  if (flags.note !== undefined) {
    comments.create({ owner: id, text: flags.note, fields: "id" });
  }
  return issues.show({ id, fields: flags.fields });
};
```

```bash
ytrack triage DEV-123 --note "Беру"
ytrack triage --help
```

- **`exports.definition`** — чистый литерал: справка, автодополнение и разбор вызова
  строятся по нему без запуска скрипта. Типы флагов: `string`, `int`, `bool`, `fields`
  (выражение полей с правилом `+`), `pair` (`--field Name=value` приходит как
  `{ Name: "value" }`) и `duration` (`PT1H30M` приходит как `90`); `multiple: true`
  делает флаг повторяемым. Неверный вызов отвергается с `bad_usage` до запуска.
- **`exports.command`** получает аргументы по порядку и объект флагов и возвращает то,
  что `ytrack` печатает одним YAML-документом.
- **`require("ytrack/v1")`** — сервисы YouTrack: `issues`, `articles`, `comments`,
  `attachments`, `links`, `tags`, `workItems`, `activities`, `projects`,
  `customFields`, `users`, а ещё `fail`, `warn` и `address`. Функция берёт один объект:
  ключ, которого нет, часть не трогает, `null` её очищает. Ответ — тот же документ, что
  печатает команда, только для чтения. Ошибка бросается с `code`, `message`, `details`
  и `wrote`, и её можно поймать.
- Декларации для редактора — [`internal/script/v1.d.ts`](internal/script/v1.d.ts);
  решения о форме — [ADR-0011](docs/adr/0011-a-command-is-a-script.md) и
  [ADR-0012](docs/adr/0012-the-script-api.md).

Скрипт работает синхронно, без `await`. Общий код кладётся в модуль того же каталога
без `exports.definition` и подключается `require("./lib/x")`. Версия API — в пути
`require`: скрипт на `ytrack/v1` не ломается, когда выходит следующая. Скрипту
доверяют как репозиторию, в котором он лежит; токен ему не виден, из входа доступен
только `address`.

## Автодополнение

`ytrack completion <оболочка>` печатает скрипт автодополнения команд, подкоманд,
флагов и их допустимых значений. Скрипт на каждый TAB спрашивает сам бинарник, поэтому
не устаревает при обновлении `ytrack`. Запросов к YouTrack автодополнение не делает:
id задач, коды проектов и имена тегов не дополняются.

```
bash:       source <(ytrack completion bash)
zsh:        source <(ytrack completion zsh)
fish:       ytrack completion fish | source
powershell: ytrack completion powershell | Out-String | Invoke-Expression
```

Чтобы дополнение работало в каждой новой оболочке, добавьте эту строку в её файл
запуска (`~/.bashrc`, `~/.zshrc` и т. п.). В bash нужен пакет `bash-completion`, в zsh
перед этим должен быть вызван `autoload -U compinit; compinit`.

## Сообщить об ошибке

Ошибки и предложения — в [issues](https://github.com/hakastein/ytrack/issues). Приложите
к отчёту вывод `ytrack --version`: версию, ревизию сборки и признак изменённой рабочей
копии. `revision: null` означает, что бинарник собран вне
git-репозитория.

## Разработка

```bash
make build      # bin/ytrack
make go         # gofmt, go vet, тесты
```

Всё знание о YouTrack — запросы, ответы, кастом-поля, ошибки, фейковый сервер — ytrack берёт из Go SDK
[`github.com/hakastein/go-youtrack`](https://github.com/hakastein/go-youtrack), а сам держит вход, печать и движок
скриптов на [goja](https://github.com/dop251/goja) с API `ytrack/v1` поверх SDK. Встроенные команды — такие же скрипты,
вшитые в бинарник: [`internal/script/builtin/`](internal/script/builtin/).
Тесты идут против фейкового сервера и живого YouTrack не требуют. Локальный YouTrack — дев-инстанс с проектом `DEV`,
на котором заведены кастом-поля всех типов: с него `make openapi` SDK снимает спеку, на нём проверяют руками и измеряют факты о сервере для ADR. Поднимается он из `dev/` одной командой, из зависимостей
нужен только docker — см. [`dev/README.md`](dev/README.md).

Документация для разработчиков:

- [`CONTEXT.md`](CONTEXT.md) — словарь предметной области;
- [`docs/adr/`](docs/adr/) — архитектурные решения;
- [`AGENTS.md`](AGENTS.md) — инструкции для AI-агентов, работающих с кодом.

Задачи и обсуждение — в [issues](https://github.com/hakastein/ytrack/issues), изменения
принимаются пулл-реквестами в `main`; CI (GitHub Actions) гоняет `make go` и сборку
на каждый push и пулл-реквест.
