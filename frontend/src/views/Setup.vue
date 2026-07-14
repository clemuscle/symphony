<template>
  <div class="setup-page">
    <div class="setup-card">
      <div class="setup-header">
        <span class="logo">🎼</span>
        <h1>{{ alreadyConfigured ? 'Réglages des providers' : 'Initialisation de Symphony' }}</h1>
        <p>Configurez les providers pour activer la plateforme</p>
      </div>

      <!-- Accès réservé à l'admin -->
      <div v-if="!isAdmin" class="not-admin">
        <p>⏳ La plateforme est en cours de configuration par votre administrateur.</p>
      </div>

      <template v-else>
        <!-- Étapes -->
        <div class="steps-indicator">
          <template v-for="(s, i) in stepDefs" :key="s.key">
            <div class="step" :class="{ active: step === i + 1, done: step > i + 1 }">{{ i + 1 }} {{ s.label }}</div>
            <div v-if="i < stepDefs.length - 1" class="step-sep">→</div>
          </template>
        </div>

        <!-- Étape 1 : SCM -->
        <div v-if="step === 1" class="form-section">
          <h2>Source Code Management</h2>

          <div class="field">
            <label>Type</label>
            <select v-model="form.scm.type">
              <option v-for="t in types.scm" :key="t" :value="t">{{ t }}</option>
            </select>
          </div>
          <div class="field">
            <label>URL</label>
            <input v-model="form.scm.url" type="url" placeholder="https://gitlab.example.com" />
          </div>
          <div class="field">
            <label>Token API</label>
            <input v-model="form.scm.token" type="password" :placeholder="tokenPlaceholder(existing.scm)" />
            <p class="field-note">
              Utilisez un <strong>Group Access Token</strong> ou <strong>Project Access Token</strong>
              scopé au groupe qui contiendra les projets et la config repo —
              <em>pas</em> un token personnel root.
              Scopes requis : <code>api</code> (création de repos) + <code>write_repository</code> (push).
            </p>
          </div>

          <div class="actions-row">
            <button class="btn-test" :disabled="testing.scm" @click="testCategory('scm')">
              {{ testing.scm ? '⏳ Test...' : '🔍 Tester la connexion' }}
            </button>
            <span v-if="testResults.scm" :class="['test-result', testResults.scm.ok ? 'ok' : 'err']">
              {{ testResults.scm.ok ? '✅ ' + testResults.scm.message : '❌ ' + testResults.scm.error }}
            </span>
          </div>

          <div class="step-nav">
            <span />
            <button class="btn-primary" :disabled="!canLeaveStep1" @click="step = 2">Suivant →</button>
          </div>
        </div>

        <!-- Étape 2 : CI -->
        <div v-if="step === 2" class="form-section">
          <h2>Intégration continue</h2>

          <div class="field">
            <label>Type</label>
            <select v-model="form.ci.type">
              <option v-for="t in types.ci" :key="t" :value="t">{{ t }}</option>
            </select>
            <p class="field-note">Utilise l'URL SCM configurée à l'étape précédente.</p>
          </div>
          <div class="field">
            <label>Dépôt de configuration <span class="hint">(ex: devops/symphony-config)</span></label>
            <input v-model="form.ci.configRepo" type="text" placeholder="devops/symphony-config" />
          </div>
          <div class="field">
            <label>Dépôt des templates <span class="hint">(ex: devops/symphony-templates)</span></label>
            <input v-model="form.ci.templatesRepo" type="text" placeholder="devops/symphony-templates" />
          </div>
          <div class="field">
            <label>Token CI <span class="hint">(optionnel)</span></label>
            <input v-model="form.ci.token" type="password" :placeholder="tokenPlaceholder(existing.ci, 'laisser vide = réutilise le token SCM')" />
            <p class="field-note">
              Par défaut le token SCM est réutilisé. Fournir un token dédié permet un scope minimal
              propre à la CI (<code>api</code>) sans partager les mêmes droits que le driver SCM.
            </p>
          </div>

          <div class="actions-row">
            <button class="btn-test" :disabled="testing.ci" @click="testCategory('ci')">
              {{ testing.ci ? '⏳ Test...' : '🔍 Tester la connexion' }}
            </button>
            <span v-if="testResults.ci" :class="['test-result', testResults.ci.ok ? 'ok' : 'err']">
              {{ testResults.ci.ok ? '✅ ' + testResults.ci.message : '❌ ' + testResults.ci.error }}
            </span>
          </div>

          <div class="step-nav">
            <button class="btn-back" @click="step = 1">← Retour</button>
            <button class="btn-primary" :disabled="!form.ci.type" @click="step = 3">Suivant →</button>
          </div>
        </div>

        <!-- Étape 3 : Registry -->
        <div v-if="step === 3" class="form-section">
          <h2>Registre d'artefacts</h2>
          <p class="field-note" style="margin: -8px 0 16px;">
            Le test de connexion interroge le registre du dépôt de configuration
            saisi à l'étape CI (<code>{{ form.ci.configRepo || '—' }}</code>) —
            c'est toujours à un projet précis que Symphony s'adresse pour le
            registre, jamais une liste générale.
          </p>

          <div class="field">
            <label>Type</label>
            <select v-model="form.registry.type">
              <option v-for="t in types.registry" :key="t" :value="t">{{ t }}</option>
            </select>
          </div>
          <div class="field">
            <label>URL du registre <span class="hint">(optionnel, déduit de l'URL SCM)</span></label>
            <input v-model="form.registry.url" type="text" placeholder="registry.gitlab.example.com:5050" />
          </div>
          <div class="field">
            <label>Token registre <span class="hint">(optionnel)</span></label>
            <input v-model="form.registry.token" type="password" :placeholder="tokenPlaceholder(existing.registry, 'laisser vide = réutilise le token SCM')" />
            <p class="field-note">
              Symphony interroge le registre via l'API GitLab
              (<code>/api/v4/.../registry/repositories</code>), pas le protocole
              Docker — le scope <code>read_registry</code>/<code>write_registry</code>
              ne s'applique pas ici (c'est pour <code>docker login</code>). Un
              token <code>read_api</code> (lecture seule) suffit à ce que
              Symphony fait aujourd'hui avec le registre.
            </p>
          </div>

          <div class="actions-row">
            <button class="btn-test" :disabled="testing.registry" @click="testCategory('registry')">
              {{ testing.registry ? '⏳ Test...' : '🔍 Tester la connexion' }}
            </button>
            <span v-if="testResults.registry" :class="['test-result', testResults.registry.ok ? 'ok' : 'err']">
              {{ testResults.registry.ok ? '✅ ' + testResults.registry.message : '❌ ' + testResults.registry.error }}
            </span>
          </div>

          <div class="step-nav">
            <button class="btn-back" @click="step = 2">← Retour</button>
            <button class="btn-primary" :disabled="!form.registry.type" @click="step = 4">Suivant →</button>
          </div>
        </div>

        <!-- Étape 4 : Déploiement -->
        <div v-if="step === 4" class="form-section">
          <h2>Déploiement</h2>

          <div class="field">
            <label>Type</label>
            <select v-model="form.deploy.type">
              <option v-for="t in types.deploy" :key="t" :value="t">{{ t }}</option>
            </select>
          </div>
          <div class="field">
            <label>Socket Docker</label>
            <input v-model="form.deploy.socket" type="text" placeholder="/var/run/docker.sock" />
          </div>

          <div class="actions-row">
            <button class="btn-test" :disabled="testing.deploy" @click="testCategory('deploy')">
              {{ testing.deploy ? '⏳ Test...' : '🔍 Tester la connexion' }}
            </button>
            <span v-if="testResults.deploy" :class="['test-result', testResults.deploy.ok ? 'ok' : 'err']">
              {{ testResults.deploy.ok ? '✅ ' + testResults.deploy.message : '❌ ' + testResults.deploy.error }}
            </span>
          </div>

          <div v-if="saveError" class="save-error">❌ {{ saveError }}</div>

          <div class="step-nav">
            <button class="btn-back" @click="step = 3">← Retour</button>
            <button class="btn-primary" :disabled="saving" @click="save">
              {{ saving ? '⏳ Enregistrement...' : '💾 Enregistrer & démarrer' }}
            </button>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { useAuth } from '../composables/useAuth'

const router = useRouter()
const { state } = useAuth()

const isAdmin = computed(() => state.user?.is_admin)

const stepDefs = [
  { key: 'scm', label: 'SCM' },
  { key: 'ci', label: 'CI' },
  { key: 'registry', label: 'Registre' },
  { key: 'deploy', label: 'Déploiement' },
]

const step = ref(1)
const saving = ref(false)
const saveError = ref('')
const alreadyConfigured = ref(false)

const types = ref({ scm: [], ci: [], registry: [], deploy: [] })
const existing = ref({ scm: {}, ci: {}, registry: {}, deploy: {} })

const form = ref({
  scm: { type: '', url: '', token: '' },
  ci: { type: '', configRepo: '', templatesRepo: '', token: '' },
  registry: { type: '', url: '', token: '' },
  deploy: { type: '', socket: '/var/run/docker.sock' },
})

const canLeaveStep1 = computed(() => form.value.scm.type && form.value.scm.url && (form.value.scm.token || existing.value.scm?.has_token))

function tokenPlaceholder(existingView, fallbackNote) {
  if (existingView?.has_token) return '•••••• (laisser vide pour ne pas changer)'
  return fallbackNote ? fallbackNote : 'glpat-xxxxxxxxxxxx'
}

onMounted(async () => {
  if (!isAdmin.value) return
  try {
    const statusRes = await api.getSetupStatus()
    alreadyConfigured.value = !!statusRes.data.configured
  } catch { /* laisser passer */ }
  try {
    const { data } = await api.getSetupConfig()
    types.value = data.types || types.value
    existing.value = data.config || existing.value
    if (data.config?.scm) {
      form.value.scm.type = data.config.scm.type || form.value.scm.type
      form.value.scm.url = data.config.scm.url || form.value.scm.url
    }
    if (data.config?.ci) {
      form.value.ci.type = data.config.ci.type || form.value.ci.type
      form.value.ci.configRepo = data.config.ci.config_repo || form.value.ci.configRepo
      form.value.ci.templatesRepo = data.config.ci.templates_repo || form.value.ci.templatesRepo
    }
    if (data.config?.registry) {
      form.value.registry.type = data.config.registry.type || form.value.registry.type
      form.value.registry.url = data.config.registry.url || form.value.registry.url
    }
    if (data.config?.deploy) {
      form.value.deploy.type = data.config.deploy.type || form.value.deploy.type
      form.value.deploy.socket = data.config.deploy.socket || form.value.deploy.socket
    }
  } catch { /* pas encore configuré, ou pas admin — formulaire vide */ }

  // Auto-sélection quand un seul type compilé existe par catégorie.
  for (const cat of ['scm', 'ci', 'registry', 'deploy']) {
    if (!form.value[cat].type && types.value[cat]?.length === 1) {
      form.value[cat].type = types.value[cat][0]
    }
  }
})

const testing = ref({ scm: false, ci: false, registry: false, deploy: false })
const testResults = ref({ scm: null, ci: null, registry: null, deploy: null })

function testConfigFor(category) {
  const f = form.value[category]
  switch (category) {
    case 'scm':
      return { url: f.url, token: f.token }
    case 'ci':
      return { url: form.value.scm.url, token: f.token || form.value.scm.token }
    case 'registry':
      return {
        scm_url: form.value.scm.url,
        url: f.url,
        token: f.token || form.value.scm.token,
        project_path: form.value.ci.configRepo,
      }
    case 'deploy':
      return { socket: f.socket }
    default:
      return {}
  }
}

async function testCategory(category) {
  testing.value[category] = true
  testResults.value[category] = null
  try {
    const r = await api.testProvider(category, form.value[category].type, testConfigFor(category))
    testResults.value[category] = r.data
  } catch (e) {
    testResults.value[category] = { ok: false, error: e.response?.data?.error || e.message }
  } finally {
    testing.value[category] = false
  }
}

// Sauvegarde finale
async function save() {
  saving.value = true
  saveError.value = ''
  try {
    await api.saveSetup({
      scm: { type: form.value.scm.type, url: form.value.scm.url, token: form.value.scm.token },
      ci: {
        type: form.value.ci.type,
        config_repo: form.value.ci.configRepo,
        templates_repo: form.value.ci.templatesRepo,
        token: form.value.ci.token,
      },
      registry: { type: form.value.registry.type, url: form.value.registry.url, token: form.value.registry.token },
      deploy: { type: form.value.deploy.type, socket: form.value.deploy.socket },
    })
    router.push('/')
  } catch (e) {
    saveError.value = e.response?.data?.error || e.message
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.setup-page {
  min-height: 100vh;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  background: #f0f2f5;
  padding: 40px 16px;
}
.setup-card {
  background: white;
  border-radius: 12px;
  padding: 40px;
  width: 100%;
  max-width: 560px;
  box-shadow: 0 4px 24px rgba(0,0,0,.08);
}
.setup-header { text-align: center; margin-bottom: 32px; }
.setup-header .logo { font-size: 40px; }
.setup-header h1 { font-size: 22px; font-weight: 700; margin: 12px 0 6px; color: #1a1a2e; }
.setup-header p { color: #888; font-size: 14px; }

.steps-indicator {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  margin-bottom: 28px;
  flex-wrap: wrap;
}
.step {
  padding: 4px 12px;
  border-radius: 20px;
  font-size: 12px;
  font-weight: 600;
  background: #f0f2f5;
  color: #888;
  white-space: nowrap;
}
.step.active { background: #667eea; color: white; }
.step.done { background: #22c55e20; color: #16a34a; }
.step-sep { color: #ccc; font-size: 12px; }

.form-section h2 { font-size: 16px; font-weight: 700; margin-bottom: 20px; color: #1a1a2e; }
.field { margin-bottom: 16px; }
.field label { display: block; font-size: 13px; font-weight: 600; color: #444; margin-bottom: 6px; }
.field .hint { font-weight: 400; color: #aaa; }
.field-note { font-size: 12px; color: #888; margin-top: 6px; line-height: 1.5; }
.field-note code { background: #f0f2f5; padding: 1px 5px; border-radius: 4px; font-size: 11px; }
.field input, .field select {
  width: 100%;
  border: 1px solid #ddd;
  border-radius: 6px;
  padding: 8px 12px;
  font-size: 14px;
  outline: none;
  transition: border-color .15s;
  background: white;
}
.field input:focus, .field select:focus { border-color: #667eea; }

.actions-row { display: flex; align-items: center; gap: 12px; margin: 20px 0; }
.btn-test {
  background: #f0f2f5;
  border: 1px solid #ddd;
  border-radius: 6px;
  padding: 7px 14px;
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
}
.btn-test:hover { background: #e8eaf0; }
.btn-test:disabled { opacity: .5; cursor: not-allowed; }
.test-result { font-size: 13px; }
.test-result.ok { color: #16a34a; }
.test-result.err { color: #dc2626; }

.step-nav { display: flex; justify-content: space-between; margin-top: 24px; }
.btn-primary {
  background: #667eea;
  color: white;
  border: none;
  border-radius: 8px;
  padding: 10px 24px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: background .15s;
}
.btn-primary:hover { background: #5a6fd6; }
.btn-primary:disabled { opacity: .5; cursor: not-allowed; }
.btn-back {
  background: transparent;
  border: 1px solid #ddd;
  border-radius: 8px;
  padding: 10px 20px;
  font-size: 14px;
  cursor: pointer;
  color: #666;
}
.btn-back:hover { background: #f0f2f5; }

.save-error { color: #dc2626; font-size: 13px; margin-top: 12px; }
.not-admin { text-align: center; color: #666; padding: 24px 0; font-size: 15px; }
</style>
