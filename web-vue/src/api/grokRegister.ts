import apiClient from './client'

// grok-register 通过 Vite proxy 的 /grok-register 路径访问
// 但 apiClient 的 baseURL 是空字符串 (相对路径), 所以直接请求 /grok-register/api/...

export type GrokRegisterStatus = {
  ok: boolean
  running: boolean
  target: number
  success: number
  fail: number
  pending: number
  warnings: number
  uncertain: number
  cancelled: boolean
  started_at: string | null
  finished_at: string | null
  accounts_file: string
  error: string
  maintenance: string | null
}

export type GrokRegisterConfig = Record<string, unknown> & {
  [key: string]: unknown
}

export type GrokRegisterLogEntry = {
  seq: number
  line: string
}

export type GrokRegisterResponse<T> = {
  ok: boolean
  [key: string]: unknown
  data?: T
}

export type ProxyPoolNode = {
  id: string
  source: string
  proxy: string
  name: string
  protocol: string
  backend: string
  enabled: boolean
  rotating: boolean
  health_model: string
  health: number
  business_samples: number
  registration_successes: number
  transport_failures: number
  suspected_failures: number
  configuration_failures: number
  exit_successes: number
  exit_failures: number
  gateway_success_rate: number
  failure_count: number
  cooldown_sec: number
  last_error: string
  last_success_at: number | null
  last_failure_at: number | null
  probe_status: string
  last_probed_at: number | null
  probe_latency_ms: number
  probe_error: string
  exit_ip: string
  ipv4_probe: {
    status: string
    tested_at: number | null
    latency_ms: number
    exit_ip: string | null
    error: string
  } | null
  ipv6_probe: {
    status: string
    tested_at: number | null
    latency_ms: number
    exit_ip: string | null
    error: string
  } | null
  inflight: number
  retired: boolean
}

export type ProxyPoolSource = {
  supported: number
  total_lines: number
  decoded_base64?: boolean
  skipped?: number
  stale?: boolean
  protocol_counts?: Record<string, number>
  error?: string
}

export type ProxyPoolStatus = {
  ok: boolean
  mode: string
  managed: boolean
  fallback: string
  capacity: number
  nodes: ProxyPoolNode[]
  sources: Record<string, ProxyPoolSource>
  runtime: Record<string, unknown>
  persist_health: boolean
}

export type ProxyPoolTestResult = {
  ok: boolean
  results: Array<{
    id: string
    status: string
    latency_ms: number
    exit_ip: string
  }>
  mode: string
  nodes: ProxyPoolNode[]
  sources: Record<string, ProxyPoolSource>
  runtime: Record<string, unknown>
}

export type OutlookMailboxAccount = {
  email: string
  mode: string
}

export type OutlookMailboxStatus = {
  ok: boolean
  path: string
  data: string
  count: number
  invalid: number
  duplicates: string[]
  accounts: OutlookMailboxAccount[]
}

export type OutlookMailboxTestResult = {
  ok: boolean
  count: number
  healthy: number
  unhealthy: number
  imap: number
  graph: number
  results: Array<{
    email: string
    mode: string
    usable: boolean
    imap: { ok: boolean; folders: string[]; error: string }
    graph: { ok: boolean; folders: string[]; error: string }
  }>
}

const BASE = '/grok-register'

export const grokRegisterApi = {
  async health(): Promise<{ ok: boolean }> {
    return (await apiClient.get<any, { ok: boolean }>(`${BASE}/health`)) as { ok: boolean }
  },

  async getStatus(): Promise<GrokRegisterStatus> {
    return (await apiClient.get<any, GrokRegisterResponse<GrokRegisterStatus>>(`${BASE}/api/status`)) as GrokRegisterStatus
  },

  async getConfig(): Promise<GrokRegisterConfig> {
    const data = await apiClient.get<any, { ok: boolean; config: GrokRegisterConfig }>(`${BASE}/api/config`)
    return data.config
  },

  async saveConfig(config: GrokRegisterConfig): Promise<GrokRegisterConfig> {
    const data = await apiClient.put<any, { ok: boolean; config: GrokRegisterConfig }>(
      `${BASE}/api/config`,
      config,
    )
    return data.config
  },

  async startRegistration(): Promise<{ ok: boolean; started: boolean; target: number; accounts_file: string }> {
    const data = await apiClient.post<any, { ok: boolean; started: boolean; target: number; accounts_file: string }>(
      `${BASE}/api/start`,
    )
    return data
  },

  async stopRegistration(): Promise<{ ok: boolean; stopped: boolean }> {
    const data = await apiClient.post<any, { ok: boolean; stopped: boolean }>(`${BASE}/api/stop`)
    return data
  },

  async getLogs(after = 0): Promise<{ ok: boolean; latest: number; entries: GrokRegisterLogEntry[] }> {
    const data = await apiClient.get<any, { ok: boolean; latest: number; entries: GrokRegisterLogEntry[] }>(
      `${BASE}/api/logs`,
      { params: { after } },
    )
    return data
  },

  // 代理池
  async getProxyPoolStatus(): Promise<ProxyPoolStatus> {
    const data = await apiClient.get<any, GrokRegisterResponse<ProxyPoolStatus>>(
      `${BASE}/api/proxy-pool/status`,
    )
    return data as unknown as ProxyPoolStatus
  },

  async reloadProxyPool(): Promise<ProxyPoolStatus> {
    const data = await apiClient.post<any, GrokRegisterResponse<ProxyPoolStatus>>(
      `${BASE}/api/proxy-pool/reload`,
    )
    return data as unknown as ProxyPoolStatus
  },

  async testProxyPool(): Promise<ProxyPoolTestResult> {
    const data = await apiClient.post<any, GrokRegisterResponse<ProxyPoolTestResult>>(
      `${BASE}/api/proxy-pool/test`,
    )
    return data as unknown as ProxyPoolTestResult
  },

  // Outlook 邮箱池
  async getOutlookMailboxes(): Promise<OutlookMailboxStatus> {
    const data = await apiClient.get<any, OutlookMailboxStatus>(`${BASE}/api/mailboxes/outlook`)
    return data
  },

  async saveOutlookMailboxes(data: string): Promise<OutlookMailboxStatus> {
    const result = await apiClient.put<any, OutlookMailboxStatus>(`${BASE}/api/mailboxes/outlook`, { data })
    return result
  },

  async testOutlookMailboxes(data: string): Promise<OutlookMailboxTestResult> {
    const result = await apiClient.post<any, OutlookMailboxTestResult>(`${BASE}/api/mailboxes/outlook/test`, { data })
    return result
  },
}
