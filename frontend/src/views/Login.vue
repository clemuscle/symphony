<template>
  <div class="login-page">
    <div class="login-card">
      <div class="brand">
        <span class="logo">🎼</span>
        <h1>Symphony</h1>
        <p>Internal Developer Portal</p>
      </div>
      <button class="btn-login" @click="login">Se connecter</button>
    </div>
  </div>
</template>

<script setup>
import { useAuth } from '../composables/useAuth'
const { login } = useAuth()
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f0f2f5;
}
.login-card {
  background: white;
  border-radius: 12px;
  padding: 48px;
  text-align: center;
  box-shadow: 0 4px 24px rgba(0,0,0,.08);
  min-width: 320px;
}
.brand .logo { font-size: 48px; }
.brand h1 { font-size: 28px; font-weight: 700; margin: 12px 0 4px; color: #1a1a2e; }
.brand p { color: #888; font-size: 14px; margin-bottom: 32px; }
.btn-login {
  background: #667eea;
  color: white;
  border: none;
  border-radius: 8px;
  padding: 12px 32px;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
  width: 100%;
  transition: background .15s;
}
.btn-login:hover { background: #5a6fd6; }
</style>
