// Capture le parcours de démo et l'assemble en GIF (documentation/demo.gif).
// Prérequis : démo lancée (./demo/up.sh) avec ses projets de démo (seed.sh).
// Se connecte en bob (developer) et crée le projet orders-api dans la GitLab
// de démo.
//   cd demo/capture && npm install
//   CHROME=/chemin/vers/chrome SYMPHONY_URL=http://localhost:8080 npm run capture
// Variables : DEMO_USER (bob), DEPLOYED (payment-api)
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
const deployed = process.env.DEPLOYED || 'payment-api'

// 0. Connexion via Keycloak (OIDC)
await page.goto(base + '/')
await page.waitForSelector('#username')
await page.fill('#username', user)
await page.fill('#password', user)
await shot(1500, 'connexion')
await page.click('#kc-login')

// 1. Projets : les services de démo, avec leur fiche et leurs déploiements
await page.waitForSelector(`.card:has-text("${deployed}") .deploy-line .dot`)
await shot(2500, 'projets')

// 2. Le détail d'un projet : CI, déploiements, fiche lue dans son repo
await page.click(`.card:has-text("${deployed}")`)
await page.waitForSelector('.panel .slo-grid')
await shot(3500, 'projet-detail')

// 3. Catalogue : les golden paths, puis création d'un projet (l'équipe est
// celle de l'utilisateur, team-payments pour bob)
await page.click('nav >> text=Catalogue')
await page.waitForSelector('.gp-card')
await shot(2000, 'catalogue')
await page.click('.gp-card:has-text("API REST Go") >> nth=0 >> .gp-cta')
await page.fill('input[placeholder="mon-api"]', 'orders-api')
await page.fill('input[placeholder="Description courte"]', 'API des commandes')
await page.locator('.form-card').scrollIntoViewIfNeeded()
await shot(2500, 'nouveau-projet')
await page.click('text=🚀 Créer le projet')
// Tâche suivie en direct : repo et projet faits, pipeline en cours, puis finie
await page.waitForSelector('.task .steps li:nth-child(3).current', { timeout: 120000 })
await page.locator('.task-wrap').scrollIntoViewIfNeeded()
await shot(3000, 'projet-cree')
await page.waitForSelector('.task.SUCCESS', { timeout: 600000 })

// 4. Déploiement en staging : image et tag préremplis depuis le registry,
// production grisée (promotion seulement)
await page.goto(base + '/deployments?project=orders-api')
await page.waitForFunction(() => document.querySelector('input[list="tags"]')?.value?.length > 0, null, { timeout: 60000 })
await page.waitForTimeout(800)
await shot(3000, 'deployer-staging')
await page.click('button:has-text("🚀 Déployer")')
await page.waitForSelector('.task-wrap .task.SUCCESS', { timeout: 300000 })
await shot(2500, 'staging')

// 5. Promotion en production : la même image, sur Kubernetes (Argo CD)
await page.goto(base + '/?project=orders-api')
await page.waitForSelector('.panel .env-item')
await page.click('.panel button:has-text("Promouvoir depuis staging")')
await page.waitForSelector('.panel .task.RUNNING, .panel .task.SUCCESS', { timeout: 60000 })
await shot(2500, 'promotion')
await page.waitForSelector('.panel .task.SUCCESS', { timeout: 300000 })
await page.waitForTimeout(16000) // relecture des déploiements (15 s)
await shot(4000, 'production')
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
