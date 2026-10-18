<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Administration</h2>
        <p class="subtitle">Ce que Symphony a chargé depuis Git, ce qu'il a refusé et pourquoi</p>
      </div>
      <div class="actions">
        <button class="btn" @click="sync" :disabled="syncing" title="Relire les stacks et les fiches, scanner les groupes de la découverte, sans attendre">
          <AppIcon name="refresh" /> {{ syncing ? 'Synchronisation…' : 'Relire Git' }}
        </button>
        <button class="btn" @click="load"><AppIcon name="restart" /> Rafraîchir</button>
      </div>
    </div>

    <div class="state err" v-if="error">{{ error }}</div>
    <div class="state" v-else-if="!status">Chargement…</div>

    <template v-else>
      <div class="summary" :class="issues.length ? 'warn' : 'ok'">
        <template v-if="issues.length">
          <strong>{{ issues.length }} point{{ issues.length > 1 ? 's' : '' }} à regarder</strong>
          <ul><li v-for="i in issues" :key="i">{{ i }}</li></ul>
        </template>
        <strong v-else><AppIcon name="ok" /> Tout est chargé, aucun provider en erreur</strong>
      </div>

      <!-- Configuration -->
      <section class="card">
        <h3><AppIcon name="config" /> Configuration</h3>
        <dl class="facts">
          <dt>Source</dt><dd><code>{{ status.config.source }}</code></dd>
          <dt>En place</dt>
          <dd><code>{{ status.config.revision }}</code>, appliquée {{ ago(status.config.applied_at) }}</dd>
          <dt>Dernière lecture</dt><dd>{{ ago(status.config.checked_at) }} <span class="muted">(toutes les 15 s)</span></dd>
        </dl>
        <div class="alert error" v-if="status.config.refused">
          <div class="alert-title">
            Version <code>{{ status.config.refused.revision }}</code> refusée {{ ago(status.config.refused.at) }} :
            la version <code>{{ status.config.revision }}</code> reste en place
          </div>
          <pre>{{ status.config.refused.error }}</pre>
          <div class="hint">Corriger dans Git ; <code>symphony validate</code> vérifie un fichier avant de le pousser.</div>
        </div>
        <div class="alert warn" v-if="status.config.read_error">
          <div class="alert-title">Source illisible : la configuration en place est conservée</div>
          <pre>{{ status.config.read_error }}</pre>
        </div>
        <div class="alert warn" v-if="status.config.secrets_error">
          <div class="alert-title">Secrets illisibles : la configuration en place est conservée</div>
          <pre>{{ status.config.secrets_error }}</pre>
        </div>
        <div class="alert warn" v-for="w in status.config.warnings || []" :key="w">
          <div class="alert-title">Appliquée, mais à corriger</div>
          <pre>{{ w }}</pre>
        </div>
      </section>

      <!-- Providers -->
      <section class="card">
        <div class="card-head">
          <h3><AppIcon name="providers" /> Providers</h3>
          <button class="btn small" @click="runChecks" :disabled="checking">
            {{ checking ? 'Vérification…' : 'Vérifier' }}
          </button>
        </div>
        <p class="muted small-text">
          Chaque provider est vérifié à l'ouverture de la page (il répond, ses identifiants sont acceptés) ;
          sinon, c'est son dernier appel qui fait foi.
        </p>
        <div class="alert error" v-if="checkError">{{ checkError }}</div>
        <table>
          <thead><tr><th>Rôle</th><th>Id</th><th>Type</th><th>État</th><th>Dernier appel</th></tr></thead>
          <tbody>
            <tr v-for="p in status.providers" :key="p.kind + '/' + p.id">
              <td>{{ kindLabels[p.kind] }}</td>
              <td><code>{{ p.id }}</code> <span class="badge" v-if="p.default">par défaut</span></td>
              <td>{{ p.type || '—' }}</td>
              <td>
                <span class="dot" :class="state(p).level"></span>
                <span :class="['state-text', state(p).level]">{{ state(p).text }}</span>
                <span class="muted" v-if="checkOf(p)"> · {{ checkOf(p)!.duration_ms }} ms</span>
              </td>
              <td class="muted">
                <template v-if="p.last_call">{{ p.last_call.method }} {{ ago(p.last_call.at) }}</template>
                <template v-else>—</template>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- Stacks -->
      <section class="card">
        <h3><AppIcon name="stacks" /> Stacks</h3>
        <dl class="facts">
          <dt>Repo</dt>
          <dd v-if="status.stacks.repo"><code>{{ status.stacks.repo }}</code>, branche <code>{{ status.stacks.branch }}</code>,
            dossier <code>{{ status.stacks.path || '.' }}</code></dd>
          <dd class="muted" v-else>aucun (section stacks sans repo)</dd>
          <dt>Officielles</dt>
          <dd>
            <template v-if="status.stacks.official.includes('*')">toutes</template>
            <span class="chip" v-else v-for="id in status.stacks.official" :key="id">{{ id }}</span>
            <span class="muted" v-if="!status.stacks.official.length">aucune (stacks.official)</span>
          </dd>
          <dt>Relues</dt><dd>{{ ago(status.stacks.loaded_at) }}</dd>
          <dt>Chargées</dt>
          <dd>
            <span class="chip" v-for="id in status.stacks.loaded" :key="id">{{ id }}</span>
            <span class="muted" v-if="!status.stacks.loaded.length">aucune</span>
          </dd>
        </dl>
        <div class="alert warn" v-if="status.stacks.error">
          <div class="alert-title">Dernière relecture en échec : la liste précédente reste proposée</div>
          <pre>{{ status.stacks.error }}</pre>
        </div>
        <div class="alert error" v-for="r in status.stacks.refused" :key="r.file">
          <div class="alert-title"><strong>{{ stackName(r.file) }}</strong> refusée <span class="muted">({{ r.file }})</span></div>
          <pre>{{ r.error }}</pre>
        </div>
        <div class="alert warn" v-for="w in status.stacks.warnings" :key="w">
          <div class="alert-title">À corriger</div>
          <pre>{{ w }}</pre>
        </div>
      </section>

      <!-- Catalogue -->
      <section class="card">
        <h3><AppIcon name="backstage" /> Fiches catalogue</h3>
        <dl class="facts">
          <dt>Fichier</dt><dd><code>{{ status.catalogue.file }}</code> à la racine du repo de chaque projet</dd>
          <dt>Relues</dt><dd>{{ ago(status.catalogue.synced_at) }}</dd>
          <dt>Sans fiche</dt>
          <dd>
            <span class="chip" v-for="p in status.catalogue.missing" :key="p">{{ p }}</span>
            <span class="muted" v-if="!status.catalogue.missing.length">aucun projet</span>
          </dd>
        </dl>
        <div class="alert warn" v-if="status.catalogue.error"><pre>{{ status.catalogue.error }}</pre></div>
        <div class="alert error" v-for="e in status.catalogue.errors" :key="e.project">
          <div class="alert-title">Fiche de <strong>{{ e.project }}</strong> illisible : la version connue reste au catalogue</div>
          <pre>{{ e.error }}</pre>
        </div>
        <div class="alert warn" v-if="status.catalogue.legacy?.length">
          <div class="alert-title">
            Fiches à l'ancien format (<code>symphony/v1</code>, lu jusqu'à la v1.0) :
            <span class="chip" v-for="p in status.catalogue.legacy" :key="p">{{ p }}</span>
          </div>
          <div class="hint">
            Les réécrire en Component Backstage (<code>apiVersion: backstage.io/v1alpha1</code>) :
            voir <code>documentation/reference/catalog-info.yaml</code>.
          </div>
        </div>
      </section>

      <!-- Découverte -->
      <section class="card">
        <h3><AppIcon name="discovered" /> Services existants</h3>
        <p class="muted" v-if="!status.discovery.enabled">
          Pas de découverte : la section <code>catalogue.discovery</code> de la configuration désigne les groupes
          GitLab ou organisations GitHub dont les repos avec une fiche deviennent des projets.
        </p>
        <template v-else>
          <dl class="facts">
            <dt>Dernier scan</dt>
            <dd>{{ status.discovery.scan_at ? ago(status.discovery.scan_at) : 'en cours' }}
              <span class="muted">(tous les {{ status.discovery.interval }})</span></dd>
          </dl>
          <table>
            <thead><tr><th>Groupe</th><th>Providers</th><th>Repos</th><th>Projets</th></tr></thead>
            <tbody>
              <tr v-for="src in status.discovery.sources" :key="src.key">
                <td><code>{{ src.group }}</code> <span class="muted">({{ src.scm }})</span></td>
                <td class="muted">{{ src.ci }}<template v-if="src.registry"> · {{ src.registry }}</template></td>
                <td v-if="src.error" colspan="2"><span class="state-text error">{{ src.error }}</span></td>
                <template v-else>
                  <td>{{ src.repos }} <span class="muted">
                    ({{ src.no_fiche }} sans fiche<template v-if="src.excluded">, {{ src.excluded }} exclu{{ src.excluded > 1 ? 's' : '' }}</template>)</span></td>
                  <td>
                    <span class="chip" v-for="p in src.projects" :key="p" :class="{ new: src.imported.includes(p) }"
                      :title="src.imported.includes(p) ? 'ajouté à ce scan' : ''">{{ p }}</span>
                    <span class="muted" v-if="!src.projects.length">aucun</span>
                  </td>
                </template>
              </tr>
            </tbody>
          </table>
        </template>
        <div class="alert warn" v-if="status.discovery.error"><pre>{{ status.discovery.error }}</pre></div>
        <div class="alert error" v-for="e in status.discovery.errors" :key="e.source + e.repo">
          <div class="alert-title"><code>{{ e.repo }}</code> <span class="muted">({{ e.source }})</span></div>
          <pre>{{ e.error }}</pre>
        </div>
        <div class="alert warn" v-for="o in status.discovery.orphans" :key="o.project">
          <div class="alert-title">
            <strong>{{ o.project }}</strong> n'est plus donné par <code>{{ o.source }}</code>
            (fiche retirée, repo exclu, archivé ou supprimé, source retirée)
          </div>
          <div class="hint">Il reste dans Symphony : le retirer depuis sa page Projets (son repo <code>{{ o.repo }}</code> est gardé).</div>
        </div>
      </section>

      <!-- Audit -->
      <section class="card">
        <h3><AppIcon name="audit" /> Journal d'audit</h3>
        <table v-if="audit.length">
          <thead><tr><th>Quand</th><th>Qui</th><th>Action</th><th>Sur</th><th>Détails</th></tr></thead>
          <tbody>
            <tr v-for="a in audit" :key="a.id">
              <td class="muted nowrap" :title="new Date(a.created_at).toLocaleString('fr-FR')">{{ ago(a.created_at) }}</td>
              <td>{{ a.user_id }}</td>
              <td><code>{{ a.action }}</code></td>
              <td>{{ a.resource }}</td>
              <td class="details">{{ a.details ? JSON.stringify(a.details) : '' }}</td>
            </tr>
          </tbody>
        </table>
        <p class="muted" v-else>Aucune entrée</p>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, errorMessage } from '../api'
import { ago, kindLabels, problems, providerState, stackName } from '../lib/admin'
import AppIcon from '../components/AppIcon.vue'
import type { AdminStatus, AuditEntry, ProviderCheck, ProviderStatus } from '../types'

const status = ref<AdminStatus | null>(null)
const audit = ref<AuditEntry[]>([])
const checks = ref<ProviderCheck[]>([])
const error = ref('')
const checkError = ref('')
const checking = ref(false)
const syncing = ref(false)

const issues = computed(() => (status.value ? problems(status.value, checks.value) : []))

const checkOf = (p: ProviderStatus) => checks.value.find(c => c.kind === p.kind && c.id === p.id)
const state = (p: ProviderStatus) => providerState(p, checkOf(p))

async function load() {
  error.value = ''
  try {
    const [s, a] = await Promise.all([api.adminStatus(), api.listAudit()])
    status.value = s.data
    audit.value = a.data
  } catch (e) {
    error.value = errorMessage(e)
  }
}

async function runChecks() {
  checking.value = true
  checkError.value = ''
  try {
    checks.value = (await api.checkProviders()).data
    // Les vérifications sont aussi des appels : le dernier appel est à jour
    status.value = (await api.adminStatus()).data
  } catch (e) {
    checkError.value = errorMessage(e)
  } finally {
    checking.value = false
  }
}

// Relit les stacks et les fiches, puis l'état une fois la relecture faite
async function sync() {
  syncing.value = true
  try {
    await api.sync()
    await new Promise(r => setTimeout(r, 1500))
    await load()
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    syncing.value = false
  }
}

onMounted(async () => {
  await load()
  if (status.value) runChecks()
})
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 20px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }
.actions { display: flex; gap: 8px; }
.btn { padding: 8px 16px; background: white; border: 1px solid #ddd; border-radius: 8px; cursor: pointer; font-size: 14px; }
.btn:disabled { opacity: .6; cursor: default; }
.btn.small { padding: 4px 12px; font-size: 13px; }
.state { color: #888; padding: 60px; text-align: center; }
.state.err { color: #c53030; }
.summary { border-radius: 10px; padding: 12px 16px; margin-bottom: 18px; font-size: 14px; }
.summary.ok { background: #f0fff4; border: 1px solid #9ae6b4; color: #276749; }
.summary.warn { background: #fffbeb; border: 1px solid #fbd38d; color: #92400e; }
.summary ul { margin: 6px 0 0 20px; }
.card { background: white; border: 1px solid #e2e2e2; border-radius: 12px; padding: 20px; margin-bottom: 16px; }
.card h3 { font-size: 16px; margin-bottom: 12px; }
.card-head { display: flex; justify-content: space-between; align-items: center; }
.card-head h3 { margin-bottom: 4px; }
.facts { display: grid; grid-template-columns: 140px 1fr; gap: 6px 12px; font-size: 14px; }
.facts dt { color: #888; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12.5px; background: #f4f4f8; padding: 1px 6px; border-radius: 4px; }
.muted { color: #888; }
.small-text { font-size: 13px; margin-bottom: 10px; }
.chip.new { background: #f0fff4; color: #276749; }
.chip { display: inline-block; font-size: 12px; padding: 2px 10px; border-radius: 20px; background: #ebf4ff; color: #4c51bf; margin: 0 6px 4px 0; }
.badge { font-size: 11px; padding: 1px 8px; border-radius: 20px; background: #f4f4f8; color: #555; margin-left: 4px; }
.alert { border-radius: 8px; padding: 10px 14px; margin-top: 12px; font-size: 13px; }
.alert.error { background: #fff5f5; border: 1px solid #feb2b2; color: #9b2c2c; }
.alert.warn { background: #fffbeb; border: 1px solid #fbd38d; color: #92400e; }
.alert-title { margin-bottom: 6px; }
.alert pre { white-space: pre-wrap; word-break: break-word; font-size: 12.5px; background: #ffffffa0; padding: 8px 10px; border-radius: 6px; }
.hint { margin-top: 6px; color: #666; }
table { width: 100%; border-collapse: collapse; font-size: 13.5px; }
th { text-align: left; color: #888; font-weight: 500; font-size: 12px; padding: 6px 8px; border-bottom: 1px solid #eee; }
td { padding: 8px; border-bottom: 1px solid #f4f4f4; vertical-align: top; }
.nowrap { white-space: nowrap; }
.details { font-family: ui-monospace, monospace; font-size: 12px; color: #666; word-break: break-all; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; vertical-align: middle; }
.dot.ok { background: #38a169; }
.dot.error { background: #e53e3e; }
.dot.unknown { background: #cbd5e0; }
.state-text.error { color: #c53030; word-break: break-word; }
.state-text.unknown { color: #888; }
</style>
