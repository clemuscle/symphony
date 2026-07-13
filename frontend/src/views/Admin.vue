<template>
  <div>
    <div class="page-header">
      <div>
        <h2>Administration</h2>
        <p class="subtitle">Configuration de la plateforme Symphony</p>
      </div>
    </div>

    <div class="admin-grid">

      <!-- Providers -->
      <div class="admin-card">
        <div class="card-title">⚙ Configuration providers</div>
        <p class="card-desc">
          Recharge <code>integrations.yaml</code> sans redémarrer Symphony.
          À utiliser après avoir modifié les tokens GitLab, l'URL du registre, ou le socket Docker.
        </p>
        <div class="card-footer">
          <button class="btn-action" :disabled="cfg.loading" @click="reloadConfig">
            {{ cfg.loading ? '⏳ Rechargement…' : '↻ Recharger la configuration' }}
          </button>
          <span v-if="cfg.ok" class="feedback ok">✓ {{ cfg.ok }}</span>
          <span v-if="cfg.err" class="feedback err">✗ {{ cfg.err }}</span>
        </div>
      </div>

      <!-- Golden paths -->
      <div class="admin-card">
        <div class="card-title">📦 Golden paths</div>
        <p class="card-desc">
          Recharge les templates depuis <code>config/golden-paths/</code> sans redémarrer.
          À utiliser après avoir ajouté ou modifié un golden path.
        </p>
        <div class="card-footer">
          <button class="btn-action" :disabled="tpl.loading" @click="reloadTemplates">
            {{ tpl.loading ? '⏳ Rechargement…' : '↻ Recharger les templates' }}
          </button>
          <span v-if="tpl.ok" class="feedback ok">✓ {{ tpl.ok }}</span>
          <span v-if="tpl.err" class="feedback err">✗ {{ tpl.err }}</span>
        </div>
      </div>

      <!-- Webhook info -->
      <div class="admin-card">
        <div class="card-title">🔗 Webhook GitLab</div>
        <p class="card-desc">
          Configurer un webhook GitLab vers Symphony pour recevoir les statuts de pipeline
          en temps réel (sans attendre la réconciliation 30s).
        </p>
        <div class="webhook-info">
          <div class="webhook-row">
            <span class="webhook-label">URL</span>
            <code class="webhook-value">{{ origin }}/api/v1/webhooks/gitlab</code>
          </div>
          <div class="webhook-row">
            <span class="webhook-label">Events</span>
            <code class="webhook-value">Pipeline events</code>
          </div>
          <div class="webhook-row">
            <span class="webhook-label">Secret</span>
            <code class="webhook-value">GITLAB_WEBHOOK_SECRET (env Symphony)</code>
          </div>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { api } from '../api'

const origin = window.location.origin.replace('5173', '8090') // dev shim

const cfg = ref({ loading: false, ok: null, err: null })
const tpl = ref({ loading: false, ok: null, err: null })

async function reloadConfig() {
  cfg.value = { loading: true, ok: null, err: null }
  try {
    const { data } = await api.reloadConfig()
    cfg.value = { loading: false, ok: data?.message || 'Providers rechargés', err: null }
  } catch (e) {
    cfg.value = { loading: false, ok: null, err: e.response?.data?.error || e.message }
  }
}

async function reloadTemplates() {
  tpl.value = { loading: true, ok: null, err: null }
  try {
    const { data } = await api.reloadTemplates()
    tpl.value = { loading: false, ok: data?.message || 'Templates rechargés', err: null }
  } catch (e) {
    tpl.value = { loading: false, ok: null, err: e.response?.data?.error || e.message }
  }
}
</script>

<style scoped>
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 28px; }
h2 { font-size: 22px; font-weight: 700; }
.subtitle { color: #888; font-size: 13px; margin-top: 4px; }

.admin-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; }

.admin-card {
  background: white;
  border: 1.5px solid #e5e7eb;
  border-radius: 14px;
  padding: 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.card-title { font-weight: 700; font-size: 15px; color: #1a1a2e; }
.card-desc { font-size: 13px; color: #666; line-height: 1.5; }
.card-desc code { background: #f0f2f5; padding: 1px 5px; border-radius: 4px; font-size: 12px; }

.card-footer { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-top: auto; }

.btn-action {
  padding: 8px 18px;
  background: #667eea;
  color: white;
  border: none;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: background .15s;
}
.btn-action:hover:not(:disabled) { background: #5a6fd6; }
.btn-action:disabled { opacity: 0.5; cursor: not-allowed; }

.feedback { font-size: 12px; font-weight: 600; }
.feedback.ok { color: #276749; }
.feedback.err { color: #c53030; }

.webhook-info { background: #f8f9fb; border-radius: 8px; padding: 12px; display: flex; flex-direction: column; gap: 8px; }
.webhook-row { display: flex; align-items: baseline; gap: 10px; }
.webhook-label { font-size: 11px; font-weight: 600; color: #888; text-transform: uppercase; min-width: 50px; }
.webhook-value { font-size: 12px; color: #444; word-break: break-all; }
</style>
