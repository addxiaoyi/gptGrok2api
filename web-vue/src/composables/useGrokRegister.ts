import { computed, ref, watch } from 'vue'
import { grokRegisterApi } from '@/api/grokRegister'
import type { GrokRegisterConfig, GrokRegisterStatus } from '@/api/grokRegister'
import { usePageRuntime } from '@/composables/usePageRuntime'
import { useVisibilityPolling } from '@/composables/usePageQuery'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/errorMessage'

// 与原 WebUI 保持一致：控制台最多保留 2000 行
const LOG_CAP = 2000
const POLL_INTERVAL_MS = 1000

type LogTone = 'ok' | 'bad' | 'info' | 'warn' | 'plain'
type LogEntry = { seq: number; line: string; tone: LogTone }

function toneOf(line: string): LogTone {
  if (line.includes('[+]')) return 'ok'
  if (line.includes('[!]')) return 'bad'
  if (line.includes('[*]')) return 'info'
  if (line.toLowerCase().includes('warn')) return 'warn'
  return 'plain'
}

export function useGrokRegister() {
  const runtime = usePageRuntime('grok-register')
  const toast = useToast()

  const isConnected = ref(false)
  const isConnecting = ref(false)
  const isDirty = ref(false)
  const notice = ref('')
  const noticeTone = ref<'info' | 'error'>('info')

  const status = ref<GrokRegisterStatus | null>(null)
  const config = ref<GrokRegisterConfig>({})
  const logs = ref<LogEntry[]>([])

  const proxyPool = ref<Awaited<ReturnType<typeof grokRegisterApi.getProxyPoolStatus>> | null>(null)
  const proxyPoolLoading = ref(false)
  const proxyPoolError = ref('')

  const mailboxPool = ref<Awaited<ReturnType<typeof grokRegisterApi.getOutlookMailboxes>> | null>(null)
  const mailboxDraft = ref('')
  const mailboxTest = ref<Awaited<ReturnType<typeof grokRegisterApi.testOutlookMailboxes>> | null>(null)
  const mailboxSaving = ref(false)

  const isRunning = computed(() => status.value?.running === true)
  const canEditConfig = computed(() => isConnected.value && !isRunning.value)
  const startDisabled = computed(() => !canEditConfig.value)
  const stopDisabled = computed(() => !isConnected.value || !isRunning.value)

  function setNotice(text: string, tone: 'info' | 'error' = 'info') {
    notice.value = text
    noticeTone.value = tone
  }

  // ── 日志轮询 ──────────────────────────────────────

  let logSeq = 0

  function applyLogs(entries: Array<{ seq: number; line: string }>, latest: number) {
    if (entries.length > 0) {
      const appended = entries.map(entry => ({ ...entry, tone: toneOf(entry.line) }))
      logs.value = [...logs.value, ...appended].slice(-LOG_CAP)
    }
    logSeq = Math.max(logSeq, latest)
  }

  async function pollStatus() {
    if (!isConnected.value) return
    try {
      const next = await grokRegisterApi.getStatus()
      status.value = next
      if (next.error) setNotice(next.error, 'error')
    } catch {
      // 轮询失败保持静默，避免日志刷屏；下次 tick 会重试
    }
  }

  async function pollLogs() {
    if (!isConnected.value) return
    try {
      const data = await grokRegisterApi.getLogs(logSeq)
      applyLogs(data.entries ?? [], data.latest ?? logSeq)
    } catch {
      // 同上
    }
  }

  const statusPolling = useVisibilityPolling({
    runtime,
    key: 'grok-register:poll',
    intervalMs: POLL_INTERVAL_MS,
    action: async () => {
      await pollStatus()
      await pollLogs()
    },
  })

  // ── 连接与配置 ────────────────────────────────────

  async function connect() {
    if (!runtime.canRun.value) return
    isConnecting.value = true
    setNotice('')
    try {
      await grokRegisterApi.health()
      const [loadedConfig, loadedStatus] = await Promise.all([
        grokRegisterApi.getConfig(),
        grokRegisterApi.getStatus(),
      ])
      config.value = { ...loadedConfig }
      status.value = loadedStatus
      isDirty.value = false
      isConnected.value = true
      statusPolling.start()
      toast.success('已连接 grok-register 服务')
    } catch (err) {
      isConnected.value = false
      setNotice(errorMessage(err, '无法连接到 grok-register 服务，请确认服务已启动'), 'error')
    } finally {
      isConnecting.value = false
    }
  }

  function patchConfig(key: string, value: unknown) {
    config.value = { ...config.value, [key]: value }
    isDirty.value = true
  }

  async function saveConfig() {
    if (!canEditConfig.value) return false
    try {
      const saved = await grokRegisterApi.saveConfig(config.value)
      config.value = { ...saved }
      isDirty.value = false
      setNotice('配置已保存')
      return true
    } catch (err) {
      toast.error(errorMessage(err, '配置保存失败'))
      return false
    }
  }

  // ── 任务控制 ──────────────────────────────────────

  async function startRegistration() {
    try {
      // 启动前先落盘未保存的修改，避免用旧配置跑任务
      if (isDirty.value && !(await saveConfig())) return
      const result = await grokRegisterApi.startRegistration()
      setNotice(`注册任务已启动，目标 ${result.target} 个账号`)
      await pollStatus()
    } catch (err) {
      toast.error(errorMessage(err, '启动注册失败'))
    }
  }

  async function stopRegistration() {
    try {
      const result = await grokRegisterApi.stopRegistration()
      setNotice(result.stopped ? '已发送停止请求' : '当前没有运行中的任务')
      await pollStatus()
    } catch (err) {
      toast.error(errorMessage(err, '停止任务失败'))
    }
  }

  // ── 日志操作 ──────────────────────────────────────

  async function copyLogs() {
    try {
      await navigator.clipboard.writeText(logs.value.map(entry => entry.line).join('\n'))
      setNotice('日志已复制')
    } catch {
      toast.error('复制失败，请检查浏览器剪贴板权限')
    }
  }

  function clearLogs() {
    logs.value = []
    logSeq = 0
    setNotice('本地日志视图已清空')
  }

  // ── 代理池 ────────────────────────────────────────

  async function loadProxyPool() {
    if (!isConnected.value) return
    proxyPoolLoading.value = true
    try {
      proxyPool.value = await grokRegisterApi.getProxyPoolStatus()
      proxyPoolError.value = ''
    } catch (err) {
      proxyPoolError.value = errorMessage(err, '加载代理池状态失败')
    } finally {
      proxyPoolLoading.value = false
    }
  }

  async function reloadProxyPool() {
    if (!isConnected.value) return
    proxyPoolLoading.value = true
    try {
      proxyPool.value = await grokRegisterApi.reloadProxyPool()
      proxyPoolError.value = ''
      toast.success('代理池已重新加载')
    } catch (err) {
      toast.error(errorMessage(err, '重载代理池失败'))
    } finally {
      proxyPoolLoading.value = false
    }
  }

  // ── Outlook 邮箱池 ────────────────────────────────

  async function loadMailboxes() {
    if (!isConnected.value) return
    try {
      const pool = await grokRegisterApi.getOutlookMailboxes()
      mailboxPool.value = pool
      mailboxDraft.value = pool.data ?? ''
    } catch (err) {
      toast.error(errorMessage(err, '加载 Outlook 邮箱池失败'))
    }
  }

  async function saveMailboxes() {
    if (!isConnected.value) return
    mailboxSaving.value = true
    try {
      const pool = await grokRegisterApi.saveOutlookMailboxes(mailboxDraft.value)
      mailboxPool.value = pool
      toast.success(`邮箱池已保存，共 ${pool.count} 个账号`)
    } catch (err) {
      toast.error(errorMessage(err, '保存 Outlook 邮箱池失败'))
    } finally {
      mailboxSaving.value = false
    }
  }

  async function testMailboxes() {
    if (!isConnected.value) return
    try {
      const result = await grokRegisterApi.testOutlookMailboxes(mailboxDraft.value)
      mailboxTest.value = result
      toast.info(`邮箱测试完成：${result.healthy}/${result.count} 可用`)
    } catch (err) {
      toast.error(errorMessage(err, '邮箱测试失败'))
    }
  }

  // ── 生命周期 ──────────────────────────────────────

  runtime.onActivate(() => {
    void connect()
  })

  runtime.onDeactivate(() => {
    statusPolling.stop()
  })

  // 连接断开后表单应立即回到可编辑的初始态
  watch(isConnected, (connected) => {
    if (connected) return
    isDirty.value = false
  })

  return {
    runtime,
    isConnected,
    isConnecting,
    isRunning,
    canEditConfig,
    startDisabled,
    stopDisabled,
    isDirty,
    notice,
    noticeTone,
    status,
    config,
    logs,
    proxyPool,
    proxyPoolLoading,
    proxyPoolError,
    mailboxPool,
    mailboxDraft,
    mailboxTest,
    mailboxSaving,
    connect,
    patchConfig,
    saveConfig,
    startRegistration,
    stopRegistration,
    copyLogs,
    clearLogs,
    loadProxyPool,
    reloadProxyPool,
    loadMailboxes,
    saveMailboxes,
    testMailboxes,
  }
}
