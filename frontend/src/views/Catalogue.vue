<template>
  <div class="catalogue-layout" :class="{ 'drawer-open': drawerOpen }">
    <div class="content">
      <div class="page-header">
        <div>
          <h2>Catalogue</h2>
          <p class="subtitle">Choisis un golden path pour démarrer un nouveau projet</p>
        </div>
      </div>

      <div v-if="error" class="error-banner">⚠️ {{ error }}</div>

      <div v-if="loading" class="gp-grid">
        <div class="gp-card skeleton" v-for="i in 4" :key="i"></div>
      </div>

      <div v-else-if="!goldenPaths.length" class="empty">
        <div class="empty-icon">📋</div>
        <div class="empty-title">Aucun golden path disponible</div>
        <div class="empty-sub">Configurez un dépôt de templates dans le wizard d'initialisation</div>
      </div>

      <div class="gp-grid" v-else>
        <div
          v-for="gp in goldenPaths"
          :key="gp.metadata.name"
          class="gp-card"
          :class="{ selected: selectedGP?.metadata.name === gp.metadata.name }"
          @click="openDrawer(gp)"
        >
          <div class="gp-icon">{{ langIcon(gp.spec.language) }}</div>
          <div class="gp-info">
            <div class="gp-name">{{ gp.metadata.name }}</div>
            <div class="gp-desc">{{ gp.metadata.description || typeLabel(gp.spec.type) }}</div>
          </div>
          <div class="gp-tags">
            <span class="tag lang">{{ gp.spec.language }}</span>
            <span class="tag type" v-if="gp.spec.type">{{ gp.spec.type }}</span>
          </div>
          <button class="btn-create" @click.stop="openDrawer(gp)">Créer un projet →</button>
        </div>
      </div>
    </div>

    <!-- Drawer de création -->
    <Transition name="drawer">
      <aside class="drawer" v-if="drawerOpen">
        <div class="drawer-header">
          <div>
            <div class="drawer-back" @click="closeDrawer">← Catalogue</div>
            <h3>
              <span class="drawer-icon">{{ langIcon(selectedGP?.spec.language) }}</span>
              {{ selectedGP?.metadata.name }}
            </h3>
          </div>
          <button class="close-btn" @click="closeDrawer">✕</button>
        </div>

        <div class="drawer-body">
          <!-- Formulaire -->
          <div v-if="!createResult" class="form-section">
            <div class="section-label">Détails du projet</div>

            <div class="field">
              <label>Nom <span class="req">*</span></label>
              <input
                v-model="form.name"
                placeholder="mon-api"
                @input="validateName"
                :class="{ error: nameError }"
              />
              <span class="field-error" v-if="nameError">{{ nameError }}</span>
            </div>

            <div class="field">
              <label>Description</label>
              <input v-model="form.description" placeholder="Courte description du projet" />
            </div>

            <div class="field">
              <label>Namespace GitLab <span class="hint">(vide = racine)</span></label>
              <input v-model="form.namespace" placeholder="mon-equipe" />
            </div>

            <div class="field">
              <label>Port applicatif</label>
              <input v-model.number="form.port" type="number" placeholder="8080" />
            </div>

            <div class="summary">
              <div class="summary-title">Symphony va créer :</div>
              <div class="summary-item">📁 Repo <code>{{ form.namespace || 'root' }}/{{ form.name || '…' }}</code></div>
              <div class="summary-item">⚙️ Pipeline CI/CD (test + build)</div>
              <div class="summary-item">📦 Entrée registre</div>
            </div>

            <div v-if="createError" class="create-error">❌ {{ createError }}</div>

            <button
              class="btn-submit"
              :disabled="creating || !form.name || !!nameError"
              @click="createProject"
            >
              {{ creating ? '⏳ Création en cours…' : '🚀 Créer le projet' }}
            </button>
          </div>

          <!-- Résultat -->
          <div v-else class="result-section">
            <div :class="['result-banner', createResult.status === 'degraded' ? 'warn' : 'ok']">
              {{ createResult.status === 'degraded' ? '⚠️ Projet créé, mais incomplet' : '✅ Projet créé avec succès' }}
            </div>

            <div class="result-links">
              <a :href="createResult.repo?.web_url" target="_blank" class="result-link">
                <span>📁 Dépôt GitLab</span><span class="link-arrow">↗</span>
              </a>
              <a :href="createResult.pipelines" target="_blank" class="result-link">
                <span>⚙️ Pipelines</span><span class="link-arrow">↗</span>
              </a>
            </div>

            <div class="steps-list">
              <div class="step-item" v-for="st in createResult.steps" :key="st.step">
                <span :class="['step-icon', st.status]">
                  {{ st.status === 'success' ? '✅' : '❌' }}
                </span>
                <span class="step-name">{{ st.step }}</span>
                <span class="step-error" v-if="st.error">{{ st.error }}</span>
              </div>
            </div>

            <div class="result-actions">
              <button class="btn-secondary" @click="goToProjects">Voir mes projets</button>
              <button class="btn-text" @click="resetForm">Créer un autre projet</button>
            </div>
          </div>
        </div>
      </aside>
    </Transition>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'

const router = useRouter()
const goldenPaths = ref([])
const loading = ref(true)
const error = ref(null)

const drawerOpen = ref(false)
const selectedGP = ref(null)
const creating = ref(false)
const createResult = ref(null)
const createError = ref(null)
const nameError = ref('')

const form = ref({ name: '', description: '', namespace: '', port: 8080 })

const langIcon = (lang) => ({ go: '🐹', python: '🐍', node: '💚', java: '☕' })[lang] || '📦'
const typeLabel = (type) => ({ rest_api: 'REST API', cli: 'CLI', worker: 'Worker', webapp: 'Web App' })[type] || type || ''

onMounted(async () => {
  try {
    const { data } = await api.getGoldenPaths()
    goldenPaths.value = data || []
  } catch (e) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
})

function openDrawer(gp) {
  selectedGP.value = gp
  drawerOpen.value = true
  createResult.value = null
  createError.value = null
  form.value = { name: '', description: '', namespace: '', port: 8080 }
}

function closeDrawer() {
  drawerOpen.value = false
  selectedGP.value = null
}

function validateName() {
  const val = form.value.name
  if (!val) { nameError.value = ''; return }
  nameError.value = /^[a-z0-9-]+$/.test(val) ? '' : 'Minuscules, chiffres et tirets uniquement'
}

async function createProject() {
  creating.value = true
  createError.value = null
  try {
    const { data } = await api.createProject({
      ...form.value,
      language: selectedGP.value.spec.language,
      type: selectedGP.value.spec.type,
    })
    createResult.value = data
  } catch (e) {
    createError.value = e.response?.data?.error || e.message
  } finally {
    creating.value = false
  }
}

function goToProjects() {
  router.push('/projects')
}

function resetForm() {
  createResult.value = null
  form.value = { name: '', description: '', namespace: '', port: 8080 }
}
</script>

<style scoped>
.catalogue-layout { position: relative; }
.content { transition: margin-right .3s ease; }
.catalogue-layout.drawer-open .content { margin-right: 440px; }

.page-header { margin-bottom: 28px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }

.error-banner { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; border-radius: 8px; padding: 10px 14px; margin-bottom: 16px; font-size: 14px; }

.gp-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 16px; }

.gp-card {
  background: white;
  border: 1.5px solid #e5e7eb;
  border-radius: 14px;
  padding: 22px;
  cursor: pointer;
  transition: all .15s;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.gp-card:hover { border-color: #667eea; box-shadow: 0 4px 16px #667eea18; transform: translateY(-2px); }
.gp-card.selected { border-color: #667eea; box-shadow: 0 0 0 3px #667eea22; }
.gp-icon { font-size: 32px; }
.gp-info { flex: 1; }
.gp-name { font-weight: 700; font-size: 15px; margin-bottom: 3px; }
.gp-desc { font-size: 13px; color: #888; line-height: 1.4; }
.gp-tags { display: flex; gap: 6px; flex-wrap: wrap; }
.tag { font-size: 11px; padding: 2px 9px; border-radius: 20px; font-weight: 500; }
.tag.lang { background: #eef2ff; color: #667eea; }
.tag.type { background: #f4f4f4; color: #666; }
.btn-create {
  margin-top: 4px;
  background: #667eea;
  color: white;
  border: none;
  border-radius: 8px;
  padding: 9px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: background .15s;
  text-align: center;
}
.btn-create:hover { background: #5a6fd6; }

/* Skeleton */
.gp-card.skeleton {
  min-height: 180px;
  background: linear-gradient(90deg, #f0f2f5 25%, #e8eaf0 50%, #f0f2f5 75%);
  background-size: 200% 100%;
  animation: shimmer 1.4s infinite;
  border: none;
  cursor: default;
  pointer-events: none;
}
@keyframes shimmer { 0% { background-position: 200% 0; } 100% { background-position: -200% 0; } }

/* Empty state */
.empty { text-align: center; padding: 80px 40px; color: #888; }
.empty-icon { font-size: 48px; margin-bottom: 16px; }
.empty-title { font-size: 16px; font-weight: 600; color: #444; margin-bottom: 6px; }
.empty-sub { font-size: 13px; }

/* Drawer */
.drawer {
  position: fixed;
  right: 0;
  top: 56px;
  width: 420px;
  height: calc(100vh - 56px);
  background: white;
  border-left: 1px solid #e5e7eb;
  box-shadow: -8px 0 32px rgba(0,0,0,.06);
  z-index: 50;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.drawer-enter-active, .drawer-leave-active { transition: transform .25s ease; }
.drawer-enter-from, .drawer-leave-to { transform: translateX(100%); }

.drawer-header {
  padding: 20px 24px 16px;
  border-bottom: 1px solid #f0f0f0;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  flex-shrink: 0;
}
.drawer-back { font-size: 12px; color: #888; cursor: pointer; margin-bottom: 6px; }
.drawer-back:hover { color: #667eea; }
.drawer-header h3 { font-size: 18px; font-weight: 700; display: flex; align-items: center; gap: 8px; }
.drawer-icon { font-size: 22px; }
.close-btn { background: none; border: none; font-size: 18px; cursor: pointer; color: #aaa; padding: 4px; }
.close-btn:hover { color: #333; }

.drawer-body { flex: 1; overflow-y: auto; padding: 24px; }

.section-label { font-size: 11px; font-weight: 700; color: #aaa; text-transform: uppercase; letter-spacing: .08em; margin-bottom: 14px; }
.field { margin-bottom: 14px; }
.field label { display: block; font-size: 13px; font-weight: 600; color: #444; margin-bottom: 5px; }
.field .hint { font-weight: 400; color: #aaa; }
.req { color: #e53e3e; }
.field input {
  width: 100%;
  border: 1.5px solid #e5e7eb;
  border-radius: 8px;
  padding: 9px 12px;
  font-size: 14px;
  outline: none;
  transition: border-color .15s;
}
.field input:focus { border-color: #667eea; }
.field input.error { border-color: #e53e3e; }
.field-error { font-size: 12px; color: #e53e3e; margin-top: 4px; }

.summary {
  background: #f8f9fb;
  border-radius: 10px;
  padding: 14px 16px;
  margin: 18px 0;
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.summary-title { font-size: 12px; font-weight: 700; color: #888; text-transform: uppercase; letter-spacing: .06em; margin-bottom: 4px; }
.summary-item { font-size: 13px; color: #444; }
.summary-item code { font-size: 12px; background: #eef2ff; color: #667eea; padding: 1px 6px; border-radius: 4px; }

.create-error { background: #fff5f5; border: 1px solid #fed7d7; border-radius: 8px; padding: 10px 14px; color: #c53030; font-size: 13px; margin-bottom: 14px; }

.btn-submit {
  width: 100%;
  background: #667eea;
  color: white;
  border: none;
  border-radius: 10px;
  padding: 12px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  transition: background .15s;
}
.btn-submit:hover { background: #5a6fd6; }
.btn-submit:disabled { opacity: .5; cursor: not-allowed; }

/* Résultat */
.result-banner {
  border-radius: 10px;
  padding: 14px 16px;
  font-weight: 700;
  font-size: 15px;
  margin-bottom: 18px;
}
.result-banner.ok { background: #f0fff4; color: #276749; }
.result-banner.warn { background: #fffbeb; color: #92400e; }

.result-links { display: flex; flex-direction: column; gap: 8px; margin-bottom: 20px; }
.result-link {
  display: flex;
  justify-content: space-between;
  align-items: center;
  color: #667eea;
  text-decoration: none;
  padding: 10px 14px;
  border-radius: 8px;
  font-size: 14px;
  border: 1px solid #e5e7eb;
  transition: background .15s;
}
.result-link:hover { background: #f0f4ff; }
.link-arrow { color: #aaa; }

.steps-list { display: flex; flex-direction: column; gap: 8px; margin-bottom: 22px; }
.step-item { display: flex; align-items: center; gap: 10px; font-size: 13px; padding: 8px 12px; background: #f8f9fb; border-radius: 8px; }
.step-name { flex: 1; color: #555; }
.step-error { font-size: 12px; color: #c53030; }

.result-actions { display: flex; flex-direction: column; gap: 10px; }
.btn-secondary {
  width: 100%;
  background: white;
  border: 1.5px solid #667eea;
  color: #667eea;
  border-radius: 10px;
  padding: 11px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: background .15s;
}
.btn-secondary:hover { background: #f0f4ff; }
.btn-text { background: none; border: none; color: #888; font-size: 13px; cursor: pointer; text-align: center; }
.btn-text:hover { color: #667eea; }
</style>
