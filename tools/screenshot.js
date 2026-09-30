/**
 * Visual verification harness.
 *
 * Drives the real SPA against the real API with a headless browser and
 * captures the main screens. It is a development tool, not part of the build.
 *
 *   node tools/screenshot.js <baseUrl> <outputDir>
 */
const fs = require('fs')
const path = require('path')
const { chromium } = require('playwright-core')

// Reuse whatever Chromium build is already on the machine instead of pinning a
// playwright revision: the harness is a dev tool, not a build dependency.
function findChromium() {
  if (process.env.CHROMIUM_PATH) return process.env.CHROMIUM_PATH
  const root = path.join(
    process.env.LOCALAPPDATA || '',
    'ms-playwright',
  )
  try {
    const dirs = fs
      .readdirSync(root)
      .filter((d) => d.startsWith('chromium-'))
      .sort()
      .reverse()
    for (const d of dirs) {
      const exe = path.join(
        root, d, 'chrome-win', 'chrome.exe',
      )
      if (fs.existsSync(exe)) return exe
    }
  } catch {
    /* fall through to the bundled default */
  }
  return undefined
}

const BASE = process.argv[2] || 'http://127.0.0.1:8790'
const OUT = process.argv[3] || './screenshots'
let TOKEN = process.argv[4] || ''

async function api(path, method = 'GET', body) {
  const res = await fetch(BASE + path, {
    method,
    headers: Object.assign(
      { 'Content-Type': 'application/json' },
      TOKEN ? { Authorization: 'Bearer ' + TOKEN } : {},
    ),
    body: body ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  try {
    return JSON.parse(text)
  } catch {
    return { raw: text }
  }
}

async function shoot(browser, name, route, opts = {}) {
  const ctx = await browser.newContext({
    viewport: { width: opts.width || 1440, height: opts.height || 900 },
    deviceScaleFactor: 2,
    colorScheme: opts.theme || 'dark',
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
  })
  const page = await ctx.newPage()
  if (TOKEN) {
    await page.addInitScript((t) => {
      localStorage.setItem('bunkr_token', t)
      localStorage.setItem('bunkr_locale', 'zh-CN')
    }, TOKEN)
  }
  const errors = []
  page.on('pageerror', (e) => errors.push(String(e)))
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text())
  })

  await page.goto(BASE + route, { waitUntil: 'networkidle' })
  await page.waitForTimeout(opts.wait || 1200)
  if (opts.act) await opts.act(page)
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: !!opts.full })
  if (errors.length) console.log(`  [${name}] console errors:`, errors.slice(0, 3))
  else console.log(`  [${name}] clean`)
  await ctx.close()
}

async function main() {
  const fs = require('fs')
  fs.mkdirSync(OUT, { recursive: true })

  // Register a fresh demo account so the authenticated screens render.
  const uname = 'shot' + Date.now().toString().slice(-6)
  const reg = await api('/api/auth/register', 'POST', {
    username: uname,
    email: uname + '@test.local',
    password: 'passw0rd',
  })
  if (!reg.token) throw new Error('register failed: ' + JSON.stringify(reg))
  TOKEN = reg.token
  const token = TOKEN

  // Membership: show the free tier on the dashboard and a paid order in history.
  await api('/api/membership/orders', 'POST', { plan: 'member_yearly' }, token)

  // Tasks.
  const ids = []
  for (const [url, auto] of [
    ['bunkr.si/a/DEMO-ANIME-EP01', true],
    ['bunkr.si/a/DEMO-PHOTOS-2024', true],
    ['bunkr.si/a/DEMO-MOVIE-1080P', true],
    ['bunkr.si/a/DEMO-MUSIC-ALBUM', true],
  ]) {
    const r = await api('/api/tasks', 'POST', { url, auto_start: true })
    if (r.task_ids) ids.push(...[].concat(r.task_ids))
  }
  if (ids.length) {
    await api(`/api/tasks/${ids[0]}/cancel`, 'POST', {}, token)
    if (ids[1]) await api(`/api/tasks/${ids[1]}/pause`, 'POST', {}, token)
  }

  const browser = await chromium.launch({ executablePath: findChromium() })
  console.log('capturing →', OUT)

  await shoot(browser, '01-login', '/login', { wait: 900 })
  await shoot(browser, '02-dashboard-dark', '/app')
  await shoot(browser, '03-dashboard-light', '/app', { theme: 'light' })
  if (ids.length) {
    await shoot(browser, '04-task-detail', '/app/tasks/' + ids[2], { wait: 1600 })
  }
  await shoot(browser, '05-membership', '/app/membership', { wait: 1000 })
  await shoot(browser, '06-settings', '/app/settings', { wait: 1000 })
  await shoot(browser, '07-new-task-modal', '/app', {
    act: async (page) => {
      await page.getByRole('button', { name: /新建任务|new task/i }).first().click()
      await page.waitForTimeout(600)
    },
  })
  await shoot(browser, '08-mobile', '/app', { width: 414, height: 880 })

  await browser.close()
  console.log('done')
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
