export type ApiError = { error?: { code?: string; message?: string } };

function csrf(): string {
  const match = document.cookie.match(/(?:^|; )cyandesk_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : "";
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (!(init.body instanceof FormData) && !headers.has("Content-Type") && init.body) {
    headers.set("Content-Type", "application/json");
  }
  headers.set("X-CyanDesk-Client", "web");
  const token = csrf();
  if (token) headers.set("X-CyanDesk-CSRF", token);
  const res = await fetch(path, { ...init, headers, credentials: "include" });
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) {
    const message = (data as ApiError).error?.message || `请求失败（${res.status}）`;
    throw new Error(message);
  }
  return data as T;
}
