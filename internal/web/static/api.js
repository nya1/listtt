// api.js: JSON fetch wrapper and toasts.

export async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (res.ok) return res.status === 204 ? null : res.json();
  let message = `${res.status} ${res.statusText}`;
  try {
    const data = await res.json();
    if (data.error) message = data.error;
  } catch { /* the body was not JSON */ }
  throw new Error(message);
}

export function toast(message) {
  const el = document.createElement('div');
  el.className = 'toast';
  el.textContent = message;
  document.getElementById('toasts').appendChild(el);
  setTimeout(() => el.remove(), 5000);
}
