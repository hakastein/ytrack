/** Пользователи полигона, кроме администратора: его заводит мастер настройки, а логин приходит из `.env`. */

export type PolygonUser = { login: string; name: string; email: string };

export type Member = PolygonUser & { group: string; role: string; timeZone: string };

/**
 * Второй токен без прав: им меряется, фильтрует ли сервер выдачу правами
 * вызывающего и что он отвечает там, где прав нет.
 */
export const LIMITED: PolygonUser = { login: "dev.limited", name: "Ограниченный", email: "dev.limited@ytrack.local" };

export const MEMBER: Member = {
  login: "dev.member",
  name: "Участник",
  email: "dev.member@ytrack.local",
  group: "Участники полигона",
  role: "contributor",
  timeZone: "Asia/Vladivostok",
};

export const INSTANCE_TIME_ZONE = "Europe/Moscow";
