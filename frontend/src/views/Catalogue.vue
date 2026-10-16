<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Catalogue</h2>
        <p class="subtitle">
          Golden paths approuvés par l'équipe plateforme : chacun crée un projet complet
          (repo, CI, image, fiche catalogue, déploiement)
        </p>
      </div>
      <button class="btn-ghost" v-if="can('admin')" :disabled="syncing" @click="syncGoldenPaths">
        {{ syncing ? 'Rechargement...' : '↻ Recharger depuis Git' }}
      </button>
    </div>

    <div v-if="loading" class="state">Chargement des golden paths...</div>
    <div class="gp-grid" v-else>
      <div v-for="gp in goldenPaths" :key="gp.id"
        class="gp-card" :class="{ selected: selected?.id === gp.id }"
        @click="select(gp)">
        <div class="gp-icon">{{ langIcon(gp.language) }}</div>
        <div class="gp-name">{{ gp.name }}</div>
        <div class="gp-desc">{{ gp.description }}</div>
        <div class="gp-tags">
          <span class="gp-tag">{{ gp.language }}</span>
          <span class="gp-tag">{{ gp.type }}</span>
          <span class="gp-tag runtime">{{ runtimeLabel(gp.runtime_provider) }}</span>
        </div>
        <button class="btn-primary gp-cta" v-if="can('developer')" @click.stop="select(gp)">Créer un projet</button>
      </div>
      <div class="empty" v-if="!goldenPaths.length">
        Aucun golden path dans <code>symphony-templates</code>
      </div>
    </div>

    <!-- Création d'un projet depuis le golden path choisi -->
    <Transition name="fade">
      <div class="form-card" v-if="selected" ref="formCard">
        <div class="form-head">
          <h3>{{ langIcon(selected.language) }} Nouveau projet : {{ selected.name }}</h3>
          <button class="close" @click="selected = null">✕</button>
        </div>

        <template v-if="can('developer')">
          <div class="section-label">Projet</div>
          <div class="form-grid">
            <div class="field">
              <label>Nom *</label>
              <input v-model="form.name" placeholder="mon-api" @input="validateName" />
              <span class="field-hint" v-if="nameError">{{ nameError }}</span>
            </div>
            <div class="field">
              <label>Description</label>
              <input v-model="form.description" placeholder="Description courte" />
            </div>
            <div class="field">
              <label>Groupe GitLab</label>
              <input v-model="form.namespace" placeholder="groupe par défaut du SCM" />
            </div>
            <div class="field">
              <label>Port de l'application</label>
              <input v-model.number="form.port" type="number" placeholder="8080" />
            </div>
          </div>

          <div class="section-label">Fiche catalogue</div>
          <div class="form-grid three">
            <div class="field">
              <label>Équipe propriétaire</label>
              <!-- Un developer choisit parmi ses équipes ; un admin, parmi celles de l'annuaire -->
              <select v-if="!can('admin') && myTeams().length" v-model="form.owner">
                <option v-for="t in myTeams()" :key="t" :value="t">{{ t }}</option>
              </select>
              <template v-else>
                <input v-model="form.owner" list="identity-groups"
                  :placeholder="groups.length ? 'équipe de l\'annuaire' : 'team-platform'" />
                <datalist id="identity-groups">
                  <option v-for="g in groups" :key="g.path" :value="g.name">{{ g.path }}</option>
                </datalist>
              </template>
            </div>
            <div class="field">
              <label>Tier</label>
              <select v-model="form.tier">
                <option value="critical">🔴 critical</option>
                <option value="standard">🟡 standard</option>
                <option value="internal">⚪ internal</option>
              </select>
            </div>
            <div class="field">
              <label>Cycle de vie</label>
              <select v-model="form.lifecycle">
                <option value="experimental">{{ lifecycleLabels.experimental }}</option>
                <option value="staging">{{ lifecycleLabels.staging }}</option>
                <option value="production">{{ lifecycleLabels.production }}</option>
              </select>
            </div>
          </div>

          <div class="summary">
            <div class="summary-title">Ce que Symphony va créer :</div>
            <div class="summary-items">
              <div class="summary-item">✅ Repo <code>{{ form.namespace || 'groupe par défaut' }}/{{ form.name || '...' }}</code> : squelette, CI <code>{{ selected.ci_template }}</code> et fiche <code>catalog-info.yaml</code> en un commit</div>
              <div class="summary-item">✅ Webhook de pipeline (suivi en direct)</div>
              <div class="summary-item">✅ Première pipeline : tests et image dans le registry</div>
              <div class="summary-item">✅ Runtime de déploiement : <code>{{ runtimeLabel(selected.runtime_provider) }}</code></div>
            </div>
          </div>

          <div class="form-footer">
            <button class="btn-primary" :disabled="creating || !form.name || !!nameError" @click="createProject">
              {{ creating ? '⏳ Envoi...' : '🚀 Créer le projet' }}
            </button>
          </div>
        </template>
        <div class="state-sm" v-else>Le rôle developer est nécessaire pour créer un projet.</div>

        <!-- Avancement de la création, en direct (tâche asynchrone) -->
        <div class="task-wrap" v-if="createTask">
          <TaskProgress :task="createTask" />
          <div class="next" v-if="createTask.status === 'SUCCESS'">
            Image construite :
            <RouterLink :to="{ path: '/deployments', query: { project: createTask.resource } }">déployer {{ createTask.resource }} →</RouterLink>
            <RouterLink :to="{ path: '/', query: { project: createTask.resource } }">voir le projet →</RouterLink>
          </div>
        </div>
        <div class="result-card err" v-if="createError">❌ {{ createError }}</div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted } from 'vue'
import { api, errorMessage } from '../api'
import { langIcon, lifecycleLabels } from '../lib/projects'
import { can, myTeams } from '../stores/session'
import TaskProgress from '../components/TaskProgress.vue'
import { taskStore, trackTask } from '../stores/tasks'
import type { CreateProjectRequest, GoldenPath, Group } from '../types'

const goldenPaths = ref<GoldenPath[]>([])
const loading = ref(true)
const syncing = ref(false)
const selected = ref<GoldenPath | null>(null)
const formCard = ref<HTMLElement | null>(null)
const creating = ref(false)
const createTaskId = ref<number | null>(null)
const createTask = computed(() => (createTaskId.value ? taskStore.byId[createTaskId.value] : undefined))
const createError = ref<string | null>(null)
const nameError = ref('')

type ProjectForm = Omit<CreateProjectRequest, 'golden_path_id' | 'scm_provider' | 'runtime_provider'>
const emptyForm = (): ProjectForm => ({
  name: '', description: '', namespace: '', port: 8080,
  owner: myTeams()[0] || '', tier: 'standard', lifecycle: 'staging',
})
const form = ref(emptyForm())

const runtimeLabels: Record<string, string> = { 'docker-local': '🐳 Docker', 'argocd-k3s': '☸️ Kubernetes (Argo CD)' }
const runtimeLabel = (rt: string) => runtimeLabels[rt] || rt || 'runtime par défaut'

// Équipes proposées pour le champ owner : groupes de l'annuaire, si le
// provider d'identité en a un (Keycloak) ; saisie libre sinon
const groups = ref<Group[]>([])
async function loadGroups() {
  try {
    const { data } = await api.listIdentityGroups()
    groups.value = data.groups || []
  } catch { groups.value = [] }
}

onMounted(() => { loadGoldenPaths(); loadGroups() })

async function select(gp: GoldenPath) {
  if (selected.value?.id !== gp.id) {
    selected.value = gp
    form.value = emptyForm()
    createTaskId.value = null
    createError.value = null
    nameError.value = ''
  }
  await nextTick()
  formCard.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function validateName() {
  const val = form.value.name
  nameError.value = val && !/^[a-z0-9-]+$/.test(val) ? 'Uniquement lettres minuscules, chiffres et tirets' : ''
}

async function loadGoldenPaths() {
  loading.value = true
  try {
    const { data } = await api.getGoldenPaths()
    goldenPaths.value = data || []
  } catch { goldenPaths.value = [] }
  finally { loading.value = false }
}

// Les golden paths sont rechargés depuis Git toutes les 15 s ; ce bouton
// force le rechargement (asynchrone côté serveur) puis relit la liste
async function syncGoldenPaths() {
  syncing.value = true
  try {
    await api.sync()
    await new Promise(r => setTimeout(r, 1500))
    await loadGoldenPaths()
  } finally { syncing.value = false }
}

// La création est une tâche : l'API répond tout de suite (202) et la
// progression arrive en direct par le flux des tâches
async function createProject() {
  creating.value = true
  createTaskId.value = null
  createError.value = null
  const gp = selected.value
  if (!gp) return
  try {
    const { data } = await api.createProject({
      ...form.value,
      golden_path_id: gp.id,
      scm_provider: gp.scm_provider,
      runtime_provider: gp.runtime_provider,
    })
    createTaskId.value = trackTask(data)
  } catch (e) {
    createError.value = errorMessage(e)
  } finally { creating.value = false }
}
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; gap: 16px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; max-width: 640px; }
.btn-primary { padding: 9px 20px; background: #667eea; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; }
.btn-primary:disabled { opacity: .5; cursor: not-allowed; }
.btn-ghost { padding: 6px 12px; background: transparent; border: 1px solid #e2e2e2; border-radius: 6px; cursor: pointer; font-size: 13px; color: #555; white-space: nowrap; }
.btn-ghost:hover { background: #f5f5f5; }
.state { color: #888; padding: 40px; text-align: center; }
.state-sm { color: #888; font-size: 13px; padding: 12px 0; }
.empty { color: #888; text-align: center; padding: 40px; grid-column: 1/-1; font-size: 14px; }
.gp-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 14px; margin-bottom: 24px; }
.gp-card { border: 2px solid #e2e2e2; border-radius: 12px; padding: 18px; cursor: pointer; transition: all .15s; background: white; display: flex; flex-direction: column; }
.gp-card:hover { border-color: #667eea; transform: translateY(-2px); box-shadow: 0 4px 12px #667eea15; }
.gp-card.selected { border-color: #667eea; background: #f0f4ff; box-shadow: 0 0 0 3px #667eea22; }
.gp-icon { font-size: 30px; margin-bottom: 8px; }
.gp-name { font-weight: 600; font-size: 15px; margin-bottom: 4px; }
.gp-desc { font-size: 13px; color: #777; margin-bottom: 12px; line-height: 1.4; flex: 1; }
.gp-tags { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 14px; }
.gp-tag { font-size: 11px; background: #eef2ff; color: #667eea; padding: 2px 8px; border-radius: 20px; }
.gp-tag.runtime { background: #f0fff4; color: #276749; }
.gp-cta { width: 100%; padding: 8px; font-size: 13px; }
.form-card { background: white; border: 1px solid #e2e2e2; border-radius: 14px; padding: 24px; scroll-margin-top: 72px; }
.form-head { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 18px; }
h3 { font-size: 17px; font-weight: 700; }
.close { background: none; border: none; font-size: 20px; cursor: pointer; color: #aaa; }
.section-label { font-size: 12px; font-weight: 600; color: #555; text-transform: uppercase; letter-spacing: .05em; margin-bottom: 10px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-bottom: 20px; }
.form-grid.three { grid-template-columns: 1fr 1fr 1fr; }
@media (max-width: 700px) { .form-grid, .form-grid.three { grid-template-columns: 1fr; } }
.field { display: flex; flex-direction: column; gap: 5px; }
.field label { font-size: 13px; font-weight: 500; color: #555; }
.field input, .field select { padding: 9px 12px; border: 1px solid #ddd; border-radius: 8px; font-size: 14px; background: white; }
.field input:focus, .field select:focus { outline: none; border-color: #667eea; }
.field-hint { font-size: 12px; color: #e53e3e; }
.summary { background: #f8f9fb; border-radius: 10px; padding: 14px 16px; margin-bottom: 16px; }
.summary-title { font-size: 13px; font-weight: 600; color: #555; margin-bottom: 10px; }
.summary-items { display: flex; flex-direction: column; gap: 6px; }
.summary-item { font-size: 13px; color: #444; }
.summary-item code, .empty code { font-size: 12px; background: #eef2ff; color: #667eea; padding: 1px 6px; border-radius: 4px; }
.form-footer { display: flex; justify-content: flex-end; }
.task-wrap { margin-top: 20px; }
.next { margin-top: 10px; font-size: 14px; display: flex; gap: 16px; flex-wrap: wrap; }
.next a { color: #667eea; text-decoration: none; font-weight: 500; }
.result-card { padding: 16px; border-radius: 10px; margin-top: 16px; }
.result-card.err { background: #fff5f5; border: 1px solid #feb2b2; color: #c53030; }
.fade-enter-active, .fade-leave-active { transition: opacity .2s, transform .2s; }
.fade-enter-from, .fade-leave-to { opacity: 0; transform: translateY(-6px); }
</style>
