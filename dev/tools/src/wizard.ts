/** Первичная настройка YouTrack: всё, для чего ещё нет ни токена, ни API. */

import { chmodSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import { chromium, type Locator, type Page } from "playwright";

const URL = process.env.YOUTRACK_URL!.replace(/\/+$/, "");
const WIZARD_TOKEN = process.env.WIZARD_TOKEN!;
const LOGIN = process.env.YOUTRACK_ADMIN_LOGIN!;
const PASSWORD = process.env.YOUTRACK_ADMIN_PASSWORD!;
const STATE_DIR = process.env.YOUTRACK_STATE_DIR ?? "/state";
const TOKEN_FILE = join(STATE_DIR, "admin-token");

const TOKEN_NAME = "ytrack-dev";
const STEP_TIMEOUT = 60_000;
const RESTART_TIMEOUT = 15 * 60 * 1000;

const step = (text: string): void => console.error(text);

function fail(code: string, text: string): never {
  console.error(`${code} ${text}`);
  process.exit(1);
}

/**
 * Формы мастера и Ring написаны на AngularJS: их валидаторы слушают посимвольный
 * ввод, а после `fill` кнопка остаётся серой.
 */
async function retype(target: Locator, value: string): Promise<void> {
  const field = target.first();
  await field.click();
  await field.clear();
  await field.pressSequentially(value);
  // Недобранное поле мастер принимает молча, а всплывает это через пять минут
  // и на другом экране — «неверный логин или пароль» после перезапуска
  const actual = await field.inputValue();
  if (actual !== value) fail("field_not_set", `в поле осталось ${JSON.stringify(actual)}`);
}

async function baseSettings(page: Page): Promise<void> {
  step("шаг 2/5: адрес и язык");
  await page.waitForSelector('input[name="baseUrl"]', { timeout: STEP_TIMEOUT });
  await retype(page.locator('input[name="baseUrl"]'), URL);
  await page.locator('[data-test="ring-select__focus"]').click();
  await page.getByText("Русский", { exact: true }).click();
  await page.locator('[anchor-id="nextButton"]').click();
}

async function adminAccount(page: Page): Promise<void> {
  step("шаг 3/5: администратор");
  await page.waitForSelector('input[name="adminLogin"]', { timeout: STEP_TIMEOUT });
  await retype(page.locator('input[name="adminLogin"]'), LOGIN);
  await retype(page.locator('input[name="password"]'), PASSWORD);
  await retype(page.locator('input[name="confirmPassword"]'), PASSWORD);
  await page.locator('[anchor-id="nextButton"]').click();
}

/**
 * Ключ на странице уже стоит: это встроенная бесплатная лицензия на десять
 * пользователей, и dev-инстансу с тремя её хватает.
 */
async function confirmLicense(page: Page): Promise<void> {
  step("шаг 4/5: лицензия");
  await page.waitForSelector('input[name="key"]', { timeout: STEP_TIMEOUT });
  await page.getByRole("button", { name: "Finish" }).click();
  // Клик по ещё не связанной кнопке проходит молча и ничего не запускает
  await page.waitForURL(/\/wait\?/, { timeout: STEP_TIMEOUT });
}

/**
 * Инстанс уходит в перезапуск, и страница мастера этого не замечает: форму входа
 * надо запрашивать заново, а не ждать её на загруженной.
 */
async function logIn(page: Page): Promise<void> {
  step("мастер закончил, инстанс перезапускается");
  const deadline = Date.now() + RESTART_TIMEOUT;
  for (;;) {
    try {
      await page.goto(`${URL}/login`, { waitUntil: "domcontentloaded" });
      // Форму отдаёт Hub после клиентского редиректа: проверка сразу после
      // `goto` не находит её никогда, а повторный `goto` редирект отменяет
      await page.waitForSelector("input#username", { timeout: STEP_TIMEOUT });
      break;
    } catch {
      // Пока идёт перезапуск, порт не слушает вовсе, а не отвечает 503
    }
    if (Date.now() > deadline) {
      fail("restart_timeout", `за ${RESTART_TIMEOUT / 1000} с инстанс не отдал форму входа`);
    }
    await sleep(5_000);
  }
  step("шаг 5/5: вход и постоянный токен");
  await retype(page.locator("input#username"), LOGIN);
  await retype(page.locator("input#password"), PASSWORD);
  await page.locator('button[type="submit"]').click();
  await page.waitForURL((url) => !url.pathname.startsWith("/hub/auth/login"), { timeout: STEP_TIMEOUT });
}

/**
 * Область доступа диалог проставляет сам — YouTrack и YouTrack Administration
 * уже стоят чипами, в выпадающем списке их поэтому нет.
 */
async function createToken(page: Page): Promise<string> {
  await page.goto(`${URL}/users/me?tab=account-security`, { waitUntil: "domcontentloaded" });
  await page.getByText("Новый токен", { exact: true }).click({ timeout: STEP_TIMEOUT });
  const dialog = page.locator('[data-test="ring-dialog"]');
  await retype(dialog.locator('input[type="text"]'), TOKEN_NAME);
  await dialog.getByRole("button", { name: "Создать" }).click();
  const shown = dialog.getByText(/perm[-:]/);
  await shown.waitFor({ timeout: STEP_TIMEOUT });
  return (await shown.innerText()).match(/perm[-:][A-Za-z0-9._=-]+/)![0];
}

async function main(): Promise<void> {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  let token: string;
  try {
    step("шаг 1/5: мастер настройки");
    await page.goto(`${URL}/?wizard_token=${WIZARD_TOKEN}`, { waitUntil: "domcontentloaded" });
    // На `/welcome` уводит уже загруженное приложение, а не ответ сервера
    const setup = page.getByRole("link", { name: "Set up" });
    try {
      await setup.waitFor({ timeout: STEP_TIMEOUT });
    } catch {
      fail("wizard_unavailable", `мастер настройки не отдан по ${page.url()}`);
    }
    await setup.click();
    await baseSettings(page);
    await adminAccount(page);
    await confirmLicense(page);
    await logIn(page);
    token = await createToken(page);
  } catch (error) {
    const shot = join(STATE_DIR, "wizard-failure.png");
    await page.screenshot({ path: shot });
    fail("wizard_stuck", `${page.url()}, снимок в ${shot}: ${String(error).split("\n")[0]}`);
  }
  await browser.close();

  writeFileSync(TOKEN_FILE, token + "\n");
  chmodSync(TOKEN_FILE, 0o600);
  console.log(`base_url: "${URL}"`);
  console.log(`login: "${LOGIN}"`);
  console.log(`token_file: "${TOKEN_FILE}"`);
}

await main();
