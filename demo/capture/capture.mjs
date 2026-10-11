// Capture le parcours de démo et l'assemble en GIF (documentation/demo.gif).
// Prérequis : démo lancée (./demo/up.sh), un service déployé (scénario
// étapes 2 et 3, hello-api par défaut). Se connecte en bob (developer) et crée
// le projet orders-api dans la GitLab de démo.
//   cd demo/capture && npm install
//   CHROME=/chemin/vers/chrome SYMPHONY_URL=http://localhost:8080 npm run capture
// Variables : DEMO_USER (bob), DEPLOYED (hello-api)
import { chromium } from 'playwright-core'
import gifenc from 'gifenc'
const { GIFEncoder, quantize, applyPalette } = gifenc
import { PNG } from 'pngjs'
import fs from 'node:fs'

const [base, chrome, out, pngDir] = process.argv.slice(2)
const W = 1200, H = 720
const browser = await chromium.launch({ executablePath: chrome })
const page = await browser.newPage({ viewport: { width: W, height: H } })
const frames = [] // { buf, delay }
let n = 0

async function shot(delay, name) {
  await page.waitForTimeout(400)
  const buf = await page.screenshot()
  frames.push({ buf, delay })
  if (name) fs.writeFileSync(`${pngDir}/${String(++n).padStart(2, '0')}-${name}.png`, buf)
}

const user = process.env.DEMO_USER || 'bob'
const deployed = process.env.DEPLOYED || 'hello-api'

// 0. Connexion via Keycloak (OIDC)
await page.goto(base + '/')
await page.waitForSelector('#username')
await page.fill('#username', user)
await page.fill('#password', user)
await shot(1500, 'connexion')
await page.click('#kc-login')

// 1. Catalogue
await page.waitForSelector('text=payment-api')
await shot(2500, 'catalogue')

// 2. Nouveau projet depuis un golden path
await page.goto(base + '/projects')
await page.click('text=+ Nouveau projet')
await page.waitForSelector('.gp-card')
await shot(1500, 'golden-paths')
await page.click('.gp-card:has-text("API REST Go")')
await page.fill('input[placeholder="mon-api"]', 'orders-api')
await page.fill('input[placeholder="Description courte"]', 'API des commandes')
await shot(2500, 'nouveau-projet')
await page.click('text=🚀 Créer le projet')
await page.waitForSelector('.result-card.ok', { timeout: 60000 })
await page.locator('.result-card.ok').scrollIntoViewIfNeeded()
await shot(3000, 'projet-cree')

// 3. Le service apparaît au catalogue
await page.goto(base + '/')
await page.waitForSelector('text=orders-api')
await shot(2000)

// 4. Déploiements : un service tourne, formulaire prérempli depuis le registry
await page.goto(base + '/deployments')
await page.waitForSelector(`text=${deployed}`)
await page.fill('input[placeholder="mon-api"]', deployed)
await page.locator('input[placeholder="mon-api"]').blur()
await page.waitForTimeout(1500)
await shot(3500, 'deploiements')

await browser.close()

// GIF : chaque capture devient une image, avec sa propre palette
const gif = GIFEncoder()
for (const { buf, delay } of frames) {
  const png = PNG.sync.read(buf)
  const palette = quantize(png.data, 256)
  gif.writeFrame(applyPalette(png.data, palette), png.width, png.height, { palette, delay })
}
gif.finish()
fs.writeFileSync(out, gif.bytes())
console.log(`${frames.length} images, ${(gif.bytes().length / 1024).toFixed(0)} Ko`)
