const { chromium } = require(process.env.HOME + '/projects/inboxQL/frontend/node_modules/playwright-core');
const fs = require('fs');
const AXE = fs.readFileSync('/private/tmp/claude-501/-Users-david-projects-inboxQL/2b3b8a5d-9756-45c1-9130-01c9a9a6558a/scratchpad/axe/package/axe.min.js', 'utf8');
const DIR = process.env.HOME + '/projects/inboxQL/docs/mockup/redesign/';
const pages = ['index','plan','today','ahead','ahead-months','replies','review','outbox','inbox','everything','subject','topic','q4','conversation','compose','query','palette','calendar','board','files','people','activity','settings','settings-accounts','settings-agents','settings-reading','settings-data','settings-computer','ask','phone-app','help','help-guide','help-here','mobile','workflows'];
(async () => {
  const b = await chromium.launch({ executablePath: process.env.HOME + '/Library/Caches/ms-playwright/chromium_headless_shell-1234/chrome-headless-shell-mac-arm64/chrome-headless-shell' });
  for (const theme of ['light', 'dark']) for (const width of [1440, 390]) {
    const p = await b.newPage({ viewport: { width, height: 900 } });
    await p.addInitScript(t => { try { localStorage.setItem('iql-mock-theme', t); } catch (e) {} }, theme);
    for (const name of pages) {
      const errs = []; p.removeAllListeners('pageerror'); p.on('pageerror', e => errs.push(e.message));
      await p.goto('file://' + DIR + name + '.html');
      await p.addScriptTag({ content: AXE });
      const r = await p.evaluate(async () => { const res = await axe.run(document, { runOnly: ['wcag2a','wcag2aa','wcag21aa','wcag22aa','best-practice'] }); return res.violations.map(v => v.id + '(' + v.nodes.length + '): ' + v.nodes.slice(0,2).map(n => n.target.join(' ')).join(' | ')); });
      const hscroll = await p.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
      if (r.length || errs.length || hscroll) console.log(theme, width, name, hscroll ? 'H-SCROLL' : '', errs, r);
      if ((width === 1440 && ['help','help-guide','help-here'].includes(name)) || (width === 390 && ['help-guide','help-here'].includes(name))) await p.screenshot({ path: 'shots/m-' + name + '-' + theme + '-' + width + '.png' });
    }
    await p.close();
  }
  console.log('done');
  await b.close();
})();
