# ytrack

Консольный клиент YouTrack, рассчитанный прежде всего на AI-агентов: компактный
машиночитаемый вывод, ошибки с кодами и команды, которые складываются пайпами.

- Один бинарник на Go, без зависимостей и без MCP-сервера.
- Весь вывод — YAML, который разбирается `yq`.
- Ошибка — тоже YAML-документ с машинным кодом, в stderr.
- Кастом-поля любых типов, включая множественные enum'ы, читаются и пишутся корректно.

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

Подробное описание каждой команды — в `ytrack <команда> <подкоманда> --help`.

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

Коды завершения: `0` — успех, `1` — ошибка, `2` — `write_uncertain`: запись ушла
на сервер, но неизвестно, применилась ли она. Повторять ли запрос, решает вызывающий,
сам `ytrack` ничего не ретраит.

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
make go         # gofmt, go vet, юнит-тесты
make contract   # контрактные тесты против dev-инстанса
make generate   # перегенерировать клиент по OpenAPI-спеке
make openapi    # обновить спеку с сервера
```

Контрактные тесты идут против локального YouTrack — дев-инстанса с проектом `DEV`, на
котором заведены кастом-поля всех типов. Поднимается он из `dev/` одной командой, из
зависимостей нужен только docker — см. [`dev/README.md`](dev/README.md).

Документация для разработчиков:

- [`CONTEXT.md`](CONTEXT.md) — словарь предметной области;
- [`docs/adr/`](docs/adr/) — архитектурные решения;
- [`AGENTS.md`](AGENTS.md) — инструкции для AI-агентов, работающих с кодом.

Задачи и обсуждение — в [issues](https://github.com/hakastein/ytrack/issues), изменения
принимаются пулл-реквестами в `main`; CI (GitHub Actions) гоняет `make go`, `make ytapi`
и сборку на каждый push и пулл-реквест.
