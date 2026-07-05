<template>
  <div class="setup-page">
    <div class="setup-card">
      <div class="setup-header">
        <span class="logo">🎼</span>
        <h1>Initialisation de Symphony</h1>
        <p>Configurez les providers pour activer la plateforme</p>
      </div>

      <!-- Accès réservé à l'admin -->
      <div v-if="!isAdmin" class="not-admin">
        <p>⏳ La plateforme est en cours de configuration par votre administrateur.</p>
      </div>

      <template v-else>
        <!-- Étapes -->
        <div class="steps-indicator">
          <div class="step" :class="{ active: step === 1, done: step > 1 }">1 GitLab</div>
          <div class="step-sep">→</div>
          <div class="step" :class="{ active: step === 2, done: step > 2 }">2 Déploiement</div>
        </div>

        <!-- Étape 1 : GitLab -->
        <div v-if="step === 1" class="form-section">
          <h2>Configuration GitLab</h2>

          <div class="field">
            <label>URL GitLab</label>
            <input v-model="form.gitlabUrl" type="url" placeholder="https://gitlab.example.com" />
          </div>
          <div class="field">
            <label>Token API</label>
            <input v-model="form.gitlabToken" type="password" placeholder="glpat-xxxxxxxxxxxx" />
            <p class="field-note">
              Utilisez un <strong>Group Access Token</strong> ou <strong>Project Access Token</strong>
              scopé au groupe qui contiendra les projets et la config repo —
              <em>pas</em> un token personnel root.
              Scopes requis : <code>api</code> (création de repos) + <code>write_repository</code> (push).
            </p>
          </div>
          <div class="field">
            <label>URL du registre <span class="hint">(optionnel, déduit de l'URL GitLab)</span></label>
            <input v-model="form.registryUrl" type="text" placeholder="registry.gitlab.example.com:5050" />
          </div>
          <div class="field">
            <label>Dépôt de configuration <span class="hint">(ex: devops/symphony-config)</span></label>
            <input v-model="form.configRepo" type="text" placeholder="devops/symphony-config" />
          </div>
          <div class="field">
            <label>Dépôt des templates <span class="hint">(ex: devops/symphony-templates)</span></label>
            <input v-model="form.templatesRepo" type="text" placeholder="devops/symphony-templates" />
          </div>

          <div class="actions-row">
            <button class="btn-test" :disabled="testingGitlab" @click="testGitlab">
              {{ testingGitlab ? '⏳ Test...' : '🔍 Tester la connexion' }}
            </button>
            <span v-if="gitlabTestResult" :class="['test-result', gitlabTestResult.ok ? 'ok' : 'err']">
              {{ gitlabTestResult.ok ? '✅ ' + gitlabTestResult.message : '❌ ' + gitlabTestResult.error }}
            </span>
          </div>

          <div class="step-nav">
            <button class="btn-primary" :disabled="!form.gitlabUrl || !form.gitlabToken" @click="step = 2">
              Suivant →
            </button>
          </div>
        </div>

        <!-- Étape 2 : Docker -->
        <div v-if="step === 2" class="form-section">
          <h2>Configuration du déploiement</h2>

          <div class="field">
            <label>Socket Docker</label>
            <input v-model="form.dockerSocket" type="text" placeholder="/var/run/docker.sock" />
          </div>

          <div class="actions-row">
            <button class="btn-test" :disabled="testingDocker" @click="testDocker">
              {{ testingDocker ? '⏳ Test...' : '🔍 Tester la connexion' }}
            </button>
            <span v-if="dockerTestResult" :class="['test-result', dockerTestResult.ok ? 'ok' : 'err']">
              {{ dockerTestResult.ok ? '✅ ' + dockerTestResult.message : '❌ ' + dockerTestResult.error }}
            </span>
          </div>

          <div v-if="saveError" class="save-error">❌ {{ saveError }}</div>

          <div class="step-nav">
            <button class="btn-back" @click="step = 1">← Retour</button>
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
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { useAuth } from '../composables/useAuth'

const router = useRouter()
const { state } = useAuth()

const isAdmin = computed(() => state.user?.is_admin)

const step = ref(1)
const saving = ref(false)
const saveError = ref('')

const form = ref({
  gitlabUrl: '',
  gitlabToken: '',
  registryUrl: '',
  configRepo: '',
  templatesRepo: '',
  dockerSocket: '/var/run/docker.sock',
})

// Test GitLab
const testingGitlab = ref(false)
const gitlabTestResult = ref(null)

async function testGitlab() {
  testingGitlab.value = true
  gitlabTestResult.value = null
  try {
    const r = await api.testProvider('gitlab', {
      url: form.value.gitlabUrl,
      token: form.value.gitlabToken,
    })
    gitlabTestResult.value = r.data
  } catch (e) {
    gitlabTestResult.value = { ok: false, error: e.response?.data?.error || e.message }
  } finally {
    testingGitlab.value = false
  }
}

// Test Docker
const testingDocker = ref(false)
const dockerTestResult = ref(null)

async function testDocker() {
  testingDocker.value = true
  dockerTestResult.value = null
  try {
    const r = await api.testProvider('docker', { socket: form.value.dockerSocket })
    dockerTestResult.value = r.data
  } catch (e) {
    dockerTestResult.value = { ok: false, error: e.response?.data?.error || e.message }
  } finally {
    testingDocker.value = false
  }
}

// Sauvegarde finale
async function save() {
  saving.value = true
  saveError.value = ''
  try {
    await api.saveSetup({
      gitlab_url: form.value.gitlabUrl,
      gitlab_token: form.value.gitlabToken,
      registry_url: form.value.registryUrl,
      config_repo: form.value.configRepo,
      templates_repo: form.value.templatesRepo,
      docker_socket: form.value.dockerSocket,
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
  gap: 8px;
  margin-bottom: 28px;
}
.step {
  padding: 4px 16px;
  border-radius: 20px;
  font-size: 13px;
  font-weight: 600;
  background: #f0f2f5;
  color: #888;
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
.field input {
  width: 100%;
  border: 1px solid #ddd;
  border-radius: 6px;
  padding: 8px 12px;
  font-size: 14px;
  outline: none;
  transition: border-color .15s;
}
.field input:focus { border-color: #667eea; }

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
