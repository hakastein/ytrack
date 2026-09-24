FROM mcr.microsoft.com/playwright:v1.62.0-noble

WORKDIR /work
# Образ несёт браузеры и системные зависимости, но не саму библиотеку; её версия
# обязана совпадать с тегом образа — ревизии браузеров прибиты к ней
COPY package.json package-lock.json tsconfig.json ./
RUN npm ci

# Инстанс отвечает по-русски, и сидирование сверяет русские имена значений
ENV LANG=C.UTF-8
