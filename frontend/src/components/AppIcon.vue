<template>
  <svg v-if="'brand' in src" class="icon" :width="size" :height="size" viewBox="0 0 24 24" role="img"
    :aria-label="src.brand.title"><title>{{ src.brand.title }}</title><path :d="src.brand.path" :fill="'#' + src.brand.hex" /></svg>
  <component v-else :is="src.picto" class="icon" :size="size" :stroke-width="2" aria-hidden="true" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { icon } from '../lib/icons'

// name : un langage, un outil (logo) ou un pictogramme de lib/icons.ts
const props = withDefaults(defineProps<{ name?: string; size?: number; fallback?: string }>(), { name: '', size: 16, fallback: 'package' })
const src = computed(() => icon(props.name, props.fallback))
</script>

<style scoped>
.icon { display: inline-block; vertical-align: -0.15em; flex-shrink: 0; }
</style>
