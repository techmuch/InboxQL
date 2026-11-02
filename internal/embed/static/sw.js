// InboxQL's service worker: what makes it installable as an app, and lets
// the window open when the computer serving it cannot be reached.
//
// Network first, always. The app is served by your own computer and changes
// with every update, so a cached copy is only ever a fallback for when that
// computer is asleep or off the network — never something served instead of
// a newer one. Mail itself (/api/) is never cached: it is not this worker's
// to keep.
const CACHE = 'inboxql-shell-v1';

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', e => {
  e.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k)))).then(() => self.clients.claim()));
});

self.addEventListener('fetch', e => {
  const req = e.request;
  const url = new URL(req.url);
  if (req.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return;
  e.respondWith(
    fetch(req).then(res => {
      if (res.ok && (req.mode === 'navigate' || url.pathname.startsWith('/assets/') || /\.(png|svg|webmanifest)$/.test(url.pathname))) {
        const copy = res.clone();
        caches.open(CACHE).then(c => c.put(req.mode === 'navigate' ? '/' : req, copy));
      }
      return res;
    }).catch(() => caches.match(req.mode === 'navigate' ? '/' : req).then(hit => hit || Response.error())),
  );
});
