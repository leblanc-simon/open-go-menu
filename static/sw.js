// Service worker: makes the application installable and usable offline
// (EI-06), offline first.
//
// - Versioned static files (?v=…) never change: once fetched, they are served
//   from the cache straight away.
// - Pages come from the network, as long as it answers within SLOW ms. Past
//   that — a shop with one bar of signal — or without network, the saved
//   copy shows at once; the network answer, when it comes, replaces the copy
//   and the page is told, to offer the latest version.
// - The pages ask for the week and the shopping list to be kept warm, so
//   that they are there offline even when not visited lately.
// Forms (POST) are never cached: the shopping list keeps its ticks itself
// and sends them once back online (app.js).

const PAGES = "opengomenu-pages";
const STATIC = "opengomenu-static";
const SLOW = 2500;

// After a form sent the usual way, the page it leads to must be the server's:
// until then, pages wait for the network however slow.
let freshUntil = 0;
// Pages served from the cache, and whether their network answer arrived
// since: url → { updated }.
const served = new Map();

self.addEventListener("install", () => self.skipWaiting());

self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));

self.addEventListener("message", (event) => {
  const data = event.data || {};
  switch (data.type) {
    case "fresh":
      freshUntil = Date.now() + 15000;
      break;
    case "hello": {
      // A page just loaded: was it the saved copy?
      const state = served.get(data.url);
      if (state) {
        event.source?.postMessage({ type: "stale", url: data.url, updated: state.updated });
      }
      break;
    }
    case "warm":
      event.waitUntil(warm(data.urls || []));
      break;
  }
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== "GET" || url.origin !== self.location.origin) {
    return;
  }
  if (url.pathname.startsWith("/static/") && url.searchParams.has("v")) {
    event.respondWith(asset(request));
  } else {
    event.respondWith(page(event));
  }
});

function cacheable(response) {
  return response.ok && response.type === "basic" && !response.redirected;
}

// asset serves a versioned static file, from the cache when it is there.
async function asset(request) {
  const cache = await caches.open(STATIC);
  const cached = await cache.match(request);
  if (cached) {
    return cached;
  }
  const response = await fetch(request);
  if (cacheable(response)) {
    // The former versions of the file are of no use any more.
    const path = new URL(request.url).pathname;
    for (const key of await cache.keys()) {
      if (new URL(key.url).pathname === path) {
        await cache.delete(key);
      }
    }
    await cache.put(request, response.clone());
  }
  return response;
}

// page serves a page from the network, or its saved copy when the network
// is slow or missing.
async function page(event) {
  const request = event.request;
  const cache = await caches.open(PAGES);
  const network = fetch(request).then(async (response) => {
    if (cacheable(response)) {
      await cache.put(request, response.clone());
    }
    return response;
  });
  const cached = await cache.match(request);
  if (!cached) {
    return network;
  }
  if (Date.now() < freshUntil) {
    return network.catch(() => cached);
  }

  let timer;
  const slow = new Promise((resolve) => {
    timer = setTimeout(() => resolve("slow"), SLOW);
  });
  const first = await Promise.race([network.catch(() => "offline"), slow]);
  clearTimeout(timer);
  if (first instanceof Response) {
    served.delete(request.url);
    return first;
  }

  const state = { updated: false };
  served.set(request.url, state);
  if (first === "slow") {
    event.waitUntil(
      network
        .then(async (response) => {
          if (!cacheable(response)) {
            return;
          }
          state.updated = true;
          for (const client of await self.clients.matchAll({ type: "window" })) {
            client.postMessage({ type: "updated", url: request.url });
          }
        })
        .catch(() => {}),
    );
  }
  return cached;
}

// warm saves the pages given, at most once every few minutes.
let warmed = 0;

async function warm(urls) {
  if (Date.now() - warmed < 10 * 60 * 1000) {
    return;
  }
  warmed = Date.now();
  const cache = await caches.open(PAGES);
  for (const url of urls) {
    try {
      const response = await fetch(url, { credentials: "same-origin" });
      if (cacheable(response)) {
        await cache.put(url, response);
      }
    } catch {
      return; // offline after all
    }
  }
}
