// Calls to houridle's control API. Changes carry the page's own header,
// which a page on another site can't send.

async function request(method, path, body) {
  const r = await fetch(path, {
    method,
    cache: "no-store",
    headers: method === "GET" ? {} : { "Content-Type": "application/json", "X-Houridle": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let data = null;
  try {
    data = await r.json();
  } catch {
    // not JSON
  }
  if (!r.ok) {
    if (r.status === 401) throw new Error("This page's key is missing or old. Open the link from houridle web again.");
    throw new Error((data && data.error) || `houridle answered HTTP ${r.status}`);
  }
  return data;
}

export const api = {
  get: (path) => request("GET", path),
  post: (path, body = {}) => request("POST", path, body),
};
