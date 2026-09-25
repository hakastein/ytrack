export type DevInstanceUser = { login: string; name: string; email: string };

export type Member = DevInstanceUser & { group: string; role: string; timeZone: string };

export const LIMITED: DevInstanceUser = { login: "dev.limited", name: "Ограниченный", email: "dev.limited@ytrack.local" };

export const MEMBER: Member = {
  login: "dev.member",
  name: "Участник",
  email: "dev.member@ytrack.local",
  group: "Участники полигона",
  role: "contributor",
  timeZone: "Asia/Vladivostok",
};

export const INSTANCE_TIME_ZONE = "Europe/Moscow";
