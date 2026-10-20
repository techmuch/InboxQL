// The screenshots in docs/images, taken from a running InboxQL.
//
//   node frontend/scripts/screenshots.mjs <base-url> <control-url> <out-dir> [scene...]
//
// Run it through `make screenshots`, which starts the devmail harness — a
// made-up mailbox on a fake Gmail — so no real mail can ever be in a picture.
// Every scene is taken twice, light and dark, as <name>.light.webp and
// <name>.dark.webp; the guides show the one that matches the reader's theme.
//
// It drives the Chrome already installed (playwright-core, no download), and
// captures through the DevTools protocol, which writes WebP directly.

import { chromium } from 'playwright-core';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import zlib from 'node:zlib';

const [base, control, out, ...only] = process.argv.slice(2);
if (!base || !control || !out) {
  console.error('usage: screenshots.mjs <base-url> <control-url> <out-dir> [scene...]');
  process.exit(2);
}

const VIEWPORT = { width: 1280, height: 760 };

// --- The mail the pictures need, delivered to the fake as another device would.

// A logo carried in the message (cid:), drawn here as a PNG: a teal tile with
// an N, at twice the size it is shown.
function logoPNG() {
  const W = 80, H = 80, r = 16;
  const px = Buffer.alloc((W * 4 + 1) * H);
  const inN = (x, y) => y >= 20 && y < 60 && ((x >= 20 && x < 29) || (x >= 51 && x < 60) ||
    Math.abs((x - 24) - (y - 20) * (31 / 40)) < 5);
  for (let y = 0; y < H; y++) {
    px[y * (W * 4 + 1)] = 0;
    for (let x = 0; x < W; x++) {
      const dx = Math.max(r - x, x - (W - 1 - r), 0), dy = Math.max(r - y, y - (H - 1 - r), 0);
      const inside = dx * dx + dy * dy <= r * r;
      const [R, G, B] = inN(x, y) ? [255, 255, 255] : [15, 118, 110];
      px.set([R, G, B, inside ? 255 : 0], y * (W * 4 + 1) + 1 + x * 4);
    }
  }
  const chunk = (type, data) => {
    const len = Buffer.alloc(4); len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type), data]);
    const crc = Buffer.alloc(4); crc.writeUInt32BE(zlib.crc32(td));
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(W, 0); ihdr.writeUInt32BE(H, 4); ihdr.set([8, 6, 0, 0, 0], 8);
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr),
    chunk('IDAT', zlib.deflateSync(px)), chunk('IEND', Buffer.alloc(0))]);
}

const shipped = () => {
  const b = 'BOUNDARY-northwind';
  const html = `<!doctype html><html><body style="font-family:Helvetica,Arial,sans-serif;color:#0f172a;max-width:560px;margin:24px auto">
<p style="display:flex;align-items:center;gap:10px;font-size:20px;font-weight:700;margin:0">
<img src="cid:logo@northwind.example" alt="" width="40" height="40">Northwind</p>
<h2 style="margin:24px 0 8px">Your order is on its way</h2>
<p>Order <b>NW-48213</b> left our warehouse this morning and should arrive on Thursday.</p>
<p><img src="https://northwind.example/banner/autumn.jpg" alt="Autumn collection" width="560" height="180" style="background:#e2e8f0"></p>
<p><a href="https://northwind.example/track/48213" style="background:#0f766e;color:#fff;padding:10px 16px;text-decoration:none">Track your parcel</a></p>
<p style="color:#64748b;font-size:12px">Questions? <a href="mailto:help@northwind.example?subject=Order%20NW-48213">Write to us</a>.</p>
<img src="https://t.northwind.example/open.gif" width="1" height="1" alt="">
</body></html>`;
  return [
    'From: Northwind Outfitters <orders@northwind.example>',
    'To: me@gmail.example',
    'Subject: Your order is on its way',
    `Date: ${new Date().toUTCString()}`,
    'Message-ID: <shipped@northwind.example>',
    'MIME-Version: 1.0',
    `Content-Type: multipart/related; boundary="${b}"`,
    '',
    `--${b}`,
    'Content-Type: text/html; charset=utf-8',
    '',
    html,
    `--${b}`,
    'Content-Type: image/png',
    'Content-ID: <logo@northwind.example>',
    'Content-Disposition: inline; filename="logo.png"',
    'Content-Transfer-Encoding: base64',
    '',
    logoPNG().toString('base64').replace(/.{76}/g, '$&\r\n'),
    `--${b}--`,
    '',
  ].join('\r\n');
};

const onHold = () => [
  'From: Account Services <alerts@secure-notice.example>',
  'To: me@gmail.example',
  'Subject: Action needed: your account is on hold',
  `Date: ${new Date().toUTCString()}`,
  'Message-ID: <hold@secure-notice.example>',
  'MIME-Version: 1.0',
  'Content-Type: text/html; charset=utf-8',
  '',
  `<!doctype html><html><body style="font-family:Helvetica,Arial,sans-serif;max-width:560px;margin:24px auto;color:#111">
<p>We noticed unusual activity and have placed a hold on your account.</p>
<p>Sign in within 24 hours to keep it open:</p>
<p style="font-size:18px"><a href="https://examplebank.account-verify.example/login">www.examplebank.com</a></p>
<p>Thank you,<br>Account Services</p></body></html>`,
  '',
].join('\r\n');

// A year of ordinary mail, so the Dashboard and the counts have a shape.
const PEOPLE = [
  ['Maya Okafor', 'maya@harbor.example', ['Lunch', 'Draft', 'Notes', 'Plans']],
  ['Tom Becker', 'tom@harbor.example', ['Budget', 'Draft', 'Review', 'Agenda']],
  ['Northwind Outfitters', 'orders@northwind.example', ['Receipt', 'Order', 'Shipping']],
  ['City Library', 'news@library.example', ['Newsletter', 'Events']],
  ['Priya Raman', 'priya@lumen.example', ['Contract', 'Invoice', 'Meeting']],
  ['Cloud Billing', 'billing@cloud.example', ['Invoice', 'Receipt']],
];
const yearOfMail = () => {
  const out = [];
  for (let i = 0; i < 90; i++) {
    const [name, addr, words] = PEOPLE[(i * 7) % PEOPLE.length];
    const when = new Date(Date.now() - (i * 3.7 + (i % 5)) * 86400000 - 3600000);
    const subject = `${words[i % words.length]} ${['for the week', 'update', 'follow-up', 'attached', 'question'][i % 5]}`;
    out.push({ raw: [
      `From: ${name} <${addr}>`, 'To: me@gmail.example', `Subject: ${subject}`,
      `Date: ${when.toUTCString()}`, `Message-ID: <seed${i}@${addr.split('@')[1]}>`,
      'MIME-Version: 1.0', 'Content-Type: text/plain; charset=utf-8', '',
      `A message about ${subject.toLowerCase()}.`, '',
    ].join('\r\n'), labels: i < 4 ? ['\\Inbox'] : /Receipt|Invoice/.test(subject) ? ['Receipts'] : [] });
  }
  return out;
};

async function deliver(raw, labels = ['\\Inbox']) {
  const r = await fetch(`${control}/deliver`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ Raw: raw, Labels: labels }),
  });
  if (!r.ok) throw new Error(`deliver: ${r.status} ${await r.text()}`);
}

// --- Helpers.

/** Every menu is in the page at once; the one open is the visible one. */
async function menu(page, top, item) {
  await page.getByText(top, { exact: true }).first().click();
  await page.locator(`text="${item}" >> visible=true`).first().click();
  // A menu can stay open under the pointer; the picture is of what it opened.
  await page.keyboard.press('Escape');
  await page.mouse.move(VIEWPORT.width - 10, VIEWPORT.height / 2);
  await page.waitForTimeout(400);
}

async function settings(page, section) {
  await page.keyboard.press('Control+,');
  await page.getByRole('button', { name: section, exact: true }).click();
  await page.waitForTimeout(600);
}

async function openMessage(page, subject) {
  await page.getByText(subject, { exact: true }).first().click();
  await page.waitForTimeout(1200);
}

const settle = page => page.waitForTimeout(700);

// --- The scenes. Each starts from a fresh layout on the Desk, does what a
// person would, and returns what to capture: nothing for the whole window,
// or a rectangle to crop to.

const SCENES = {
  desk: async () => {},

  query: async page => {
    const q = page.getByRole('textbox', { name: 'Query' });
    await q.fill('folder:allmail | count by label');
    await page.getByRole('button', { name: 'Run' }).click();
    await page.waitForTimeout(1200);
    return { x: 286, y: 76, width: 994, height: 400 };
  },

  'load-images': async page => {
    await openMessage(page, 'Your order is on its way');
  },

  'link-warning': async page => {
    await openMessage(page, 'Action needed: your account is on hold');
    const frame = page.frameLocator('iframe').first();
    await frame.getByText('www.examplebank.com').hover();
    await page.waitForTimeout(400);
  },

  'label-picker': async page => {
    await openMessage(page, 'Your invoice from Acme');
    await page.getByRole('button', { name: /^Label/ }).first().click();
    await page.waitForTimeout(500);
    return { x: 150, y: 76, width: 1130, height: 380 };
  },

  approvals: async page => {
    await menu(page, 'Intelligence', 'Waiting for approval');
    await settle(page);
    return { x: 250, y: 90, width: 810, height: 470 };
  },

  'settings-general': async page => { await settings(page, 'General'); },
  // Cropped above the Login service card: launchctl answers for the machine,
  // not the home folder, so below it would be this computer's own service.
  'settings-system': async page => {
    await settings(page, 'System');
    await page.waitForTimeout(800);
    return { x: 0, y: 0, width: VIEWPORT.width, height: 600 };
  },
  'mail-accounts': async page => {
    await settings(page, 'Mail Accounts');
    await page.waitForTimeout(800);
    return { x: 290, y: 76, width: 990, height: 330 };
  },

  log: async page => {
    await page.keyboard.press('Control+Shift+E');
    await settle(page);
    return { x: 31, y: 76, width: 1249, height: 200 };
  },

  dashboard: async page => {
    await page.getByRole('textbox', { name: 'Query' }).fill('folder:allmail');
    await page.getByRole('button', { name: 'Run' }).click();
    await settle(page);
    await page.keyboard.press('Control+Shift+D');
    await page.waitForTimeout(1500);
  },

  // Every earlier scene was a window of its own; only this one is shown.
  windows: async page => {
    await menu(page, 'View', 'Windows');
    await settle(page);
    const me = (await page.getByText(/^this one is/).innerText()).replace(/\D/g, '');
    const { windows = [] } = await (await fetch(`${base}/api/ui`)).json();
    for (const w of windows) if (String(w.name) !== me) await fetch(`${base}/api/ui/${w.name}`, { method: 'DELETE' });
    await page.getByTitle('Refresh').click();
    await settle(page);
    return { x: 31, y: 76, width: 1249, height: 180 };
  },
};

// --- Run.

await mkdir(out, { recursive: true });
for (const m of yearOfMail()) await deliver(m.raw, m.labels);
await deliver(shipped());
await deliver(onHold());
// Live sync brings them in within seconds.
await new Promise(r => setTimeout(r, 6000));

const browser = await chromium.launch({ channel: 'chrome' });
const names = only.length ? only : Object.keys(SCENES);
let failed = 0;
for (const theme of ['light', 'dark']) {
  const context = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 1 });
  await context.addInitScript(t => {
    try { localStorage.setItem('nexus-shell-theme', t); } catch { /* a fresh origin */ }
  }, theme);
  for (const name of names) {
    const page = await context.newPage();
    try {
      await page.goto(base);
      await page.evaluate(() => localStorage.removeItem('nexus-shell-layout'));
      await page.reload();
      await page.getByRole('textbox', { name: 'Query' }).waitFor();
      await settle(page);
      const clip = await SCENES[name](page);
      const cdp = await context.newCDPSession(page);
      const { data } = await cdp.send('Page.captureScreenshot', {
        format: 'webp', quality: 82, ...(clip ? { clip: { ...clip, scale: 1 } } : {}),
      });
      const file = join(out, `${name}.${theme}.webp`);
      await writeFile(file, Buffer.from(data, 'base64'));
      console.log('wrote', file);
    } catch (e) {
      failed++;
      console.error(`${name} (${theme}): ${e.message.split('\n')[0]}`);
    } finally {
      await page.close();
    }
  }
  await context.close();
}
await browser.close();
process.exit(failed ? 1 : 0);
