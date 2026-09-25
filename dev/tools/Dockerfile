FROM mcr.microsoft.com/playwright:v1.62.0-noble

WORKDIR /work
# Версия playwright в package.json — та же, что в теге образа: к ней прибиты ревизии браузеров
COPY package.json package-lock.json tsconfig.json ./
RUN npm ci

ENV LANG=C.UTF-8
