---
status: superseded by go-youtrack ADR-0004
---

# Генератор владеет поверхностью операций

Генератор, спецификация YouTrack и её overlay принадлежат SDK:
[ADR-0004 SDK](https://github.com/hakastein/go-youtrack/blob/main/docs/adr/0004-the-generator-owns-the-operation-surface.md).
ytrack зовёт только сервисы клиента SDK и не видит ни сгенерированного клиента `ytapi`, ни `Client.API()`.
