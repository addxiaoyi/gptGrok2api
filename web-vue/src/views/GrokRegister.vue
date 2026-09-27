<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getAuthToken } from '@/api/client'

const GROK_REGISTER_BASE_URL = import.meta.env.VITE_GROK_REGISTER_URL || 'http://127.0.0.1:8092'

const iframeSrc = computed(() => {
  const token = getAuthToken()
  return `${GROK_REGISTER_BASE_URL}?admin_token=${encodeURIComponent(token)}`
})

const isLoading = ref(true)
const isConnected = ref(false)
const iframeKey = ref(0)
const error = ref<string | null>(null)

function handleLoad() {
  isLoading.value = false
  isConnected.value = true
  error.value = null
}

function handleError() {
  isLoading.value = false
  isConnected.value = false
  error.value = '无法连接到 Grok 注册机服务'
}

function reload() {
  isLoading.value = true
  error.value = null
  iframeKey.value += 1
}

onMounted(() => {
  setTimeout(() => {
    if (isLoading.value) {
      isLoading.value = false
      isConnected.value = false
      error.value = '连接超时，请检查 grok-register 服务是否运行'
    }
  }, 10000)
})
</script>

<template>
  <div class="grok-register-page">
    <div v-if="!isConnected && !isLoading" class="connection-error">
      <div class="error-icon">⚠️</div>
      <h3>{{ error }}</h3>
      <p>
        请先启动 grok-register 服务：<br />
        <code>docker compose -f integrations/docker-compose.yml up -d</code>
      </p>
      <button class="retry-btn" @click="reload">重试连接</button>
    </div>

    <iframe
      v-show="isConnected || isLoading"
      :key="iframeKey"
      :src="iframeSrc"
      class="grok-register-iframe"
      @load="handleLoad"
      @error="handleError"
      frameborder="0"
    />

    <div v-if="isLoading" class="loading-overlay">
      <div class="spinner"></div>
      <p>正在连接 Grok 注册机...</p>
    </div>
  </div>
</template>

<style scoped>
.grok-register-page {
  width: 100%;
  height: 100%;
  position: relative;
  background: hsl(var(--background));
}

.grok-register-iframe {
  width: 100%;
  height: 100%;
  border: none;
  display: block;
}

.loading-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: hsl(var(--background));
  z-index: 10;
}

.spinner {
  width: 48px;
  height: 48px;
  border: 4px solid hsl(var(--muted));
  border-top-color: hsl(var(--primary));
  border-radius: 50%;
  animation: spin 1s linear infinite;
  margin-bottom: 16px;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.connection-error {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 24px;
}

.error-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.connection-error h3 {
  font-size: 20px;
  font-weight: 600;
  color: hsl(var(--foreground));
  margin: 0 0 8px 0;
}

.connection-error p {
  font-size: 14px;
  color: hsl(var(--muted-foreground));
  margin: 0 0 16px 0;
}

.connection-error code {
  display: block;
  background: hsl(var(--muted));
  color: hsl(var(--accent-foreground));
  padding: 8px 12px;
  border-radius: 6px;
  font-family: ui-monospace, SFMono-Regular, monospace;
  font-size: 13px;
  margin-top: 8px;
}

.retry-btn {
  margin-top: 16px;
  padding: 10px 24px;
  background: hsl(var(--primary));
  color: hsl(var(--primary-foreground));
  border: none;
  border-radius: 6px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s;
}

.retry-btn:hover {
  background: hsl(var(--primary) / 0.9);
}
</style>
