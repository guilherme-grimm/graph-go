// Measures graph-go UI cost at scale in headless Chromium: time until nodes
// render, rendered node/edge DOM counts, JS heap, frame rate while panning,
// and WebSocket traffic plus main-thread long-task time over an idle 15s
// window (the cost of live health updates).
//
// Usage (from the repo root, after `npm ci` in webui/):
//   node hack/scale/measure.mjs <url> [swimlane|hierarchical|force]
// Set SHOT=/path/to/file.png to save a screenshot once the graph settles.
// Set CHROMIUM=/path/to/chromium to use a system browser instead of
// Playwright's bundled one.
import { createRequire } from 'node:module';

const require = createRequire(new URL('../../webui/package.json', import.meta.url));
const { chromium } = require('playwright');

const URL_ = process.argv[2] ?? 'http://localhost:8080/';
const LAYOUT = process.argv[3] ?? 'swimlane';

const measure = async (page) => {

  await page.addInitScript((layout) => {
    try {
      localStorage.clear();
      localStorage.setItem('graph-layout-mode', layout);
    } catch {}
    window.__ws = { msgs: 0, bytes: 0 };
    const Orig = window.WebSocket;
    window.WebSocket = class extends Orig {
      constructor(...a) {
        super(...a);
        this.addEventListener('message', (e) => {
          window.__ws.msgs++;
          window.__ws.bytes += e.data.length;
        });
      }
    };
    window.__longTasks = 0;
    new PerformanceObserver((l) => {
      for (const e of l.getEntries()) window.__longTasks += e.duration;
    }).observe({ type: 'longtask', buffered: true });
  }, LAYOUT);

  const t0 = Date.now();
  await page.goto(URL_);
  await page.waitForSelector('.react-flow__node', { timeout: 120000 });
  const firstRenderMs = Date.now() - t0;
  await page.waitForTimeout(3000);
  if (process.env.SHOT) await page.screenshot({ path: process.env.SHOT });

  const counts = await page.evaluate(() => ({
    domNodes: document.querySelectorAll('.react-flow__node').length,
    domEdges: document.querySelectorAll('.react-flow__edge').length,
    heapMB: Math.round((performance.memory?.usedJSHeapSize ?? 0) / 1048576),
    loadLongTaskMs: Math.round(window.__longTasks),
    layoutLabel: document.querySelector('button[aria-haspopup="listbox"]')?.textContent?.trim(),
  }));

  // Pan for ~3s and count animation frames.
  const box = await page.locator('.react-flow__pane').boundingBox();
  const cx = box.x + box.width / 2, cy = box.y + box.height / 2;
  await page.evaluate(() => {
    window.__frames = 0;
    window.__panning = true;
    const tick = () => { window.__frames++; if (window.__panning) requestAnimationFrame(tick); };
    requestAnimationFrame(tick);
  });
  const panStart = Date.now();
  await page.mouse.move(cx, cy);
  await page.mouse.down();
  for (let i = 0; i < 60; i++) {
    const dx = Math.sin(i / 6) * 300, dy = Math.cos(i / 6) * 150;
    await page.mouse.move(cx + dx, cy + dy, { steps: 2 });
  }
  await page.mouse.up();
  const panMs = Date.now() - panStart;
  const frames = await page.evaluate(() => { window.__panning = false; return window.__frames; });

  // Idle window: what live updates cost while nobody touches the page.
  await page.evaluate(() => { window.__ws.msgs = 0; window.__ws.bytes = 0; window.__longTasks = 0; });
  await page.waitForTimeout(15000);
  const idle = await page.evaluate(() => ({
    wsMsgs: window.__ws.msgs,
    wsKB: Math.round(window.__ws.bytes / 1024),
    longTaskMs: Math.round(window.__longTasks),
    heapMB: Math.round((performance.memory?.usedJSHeapSize ?? 0) / 1048576),
  }));

  return {
    layout: LAYOUT,
    firstRenderMs,
    ...counts,
    panFps: Math.round((frames / panMs) * 1000),
    idle15s: idle,
  };
};

const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM || undefined,
  args: ['--enable-precise-memory-info'],
});
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
try {
  console.log(JSON.stringify(await measure(page)));
} finally {
  await browser.close();
}
