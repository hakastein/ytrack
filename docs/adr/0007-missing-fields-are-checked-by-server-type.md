---
status: superseded by go-youtrack ADR-0006
---

# Отсутствующее поле проверяется по типу, который назвал сервер

Когда отсутствующий ключ ответа даёт `unknown_name`, когда `upstream_invalid`, а когда ошибки нет, и как устроен
каталог схем, решает SDK:
[ADR-0006 SDK](https://github.com/hakastein/go-youtrack/blob/main/docs/adr/0006-a-document-is-a-tree-under-the-callers-fields.md).
ytrack печатает его ошибку как есть ([ADR-0005](0005-an-error-is-a-document.md)).
