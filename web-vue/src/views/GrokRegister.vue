<template>
  <div class="grok-register-native">
    <PagePanel class="space-y-4">
      <PanelHeader title="Grok 注册机" align="start">
        <template #copy>
          <p class="mt-1 text-xs text-muted-foreground">
            直接通过主项目代理访问 grok-register 服务，配置、任务控制与日志终端均内嵌于此。
          </p>
        </template>
        <template #actions>
          <StatusPill
            :tone="isConnected ? 'success' : 'neutral'"
            variant="soft"
            size="sm"
            :label="isConnected ? '已连接' : '未连接'"
          />
          <Button size="sm" variant="outline" :disabled="isConnecting" @click="connect">
            {{ isConnecting ? '连接中...' : '重新连接' }}
          </Button>
        </template>
      </PanelHeader>

      <PageLoadingState
        v-if="isConnecting && !isConnected"
        title="正在连接 Grok 注册机..."
        description="读取服务健康状态与配置。"
      />

      <div
        v-else-if="!isConnected"
        class="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
      >
        {{ notice }}
      </div>

      <template v-else>
        <div v-if="notice" :class="`text-sm ${noticeTone === 'error' ? 'text-destructive' : 'text-muted-foreground'}`">
          {{ notice }}
        </div>

        <div class="grid gap-3 md:grid-cols-4">
          <InfoCard tag="div" title="任务状态" density="compact">
            <StatusPill
              :tone="isRunning ? 'success' : status?.cancelled ? 'warning' : 'neutral'"
              variant="soft"
              :label="isRunning ? '运行中' : status?.cancelled ? '已停止' : '待机中'"
            />
          </InfoCard>
          <InfoCard tag="div" title="目标数量" density="compact">
            <p class="text-2xl font-bold tabular-nums text-foreground">{{ status?.target ?? 0 }}</p>
          </InfoCard>
          <InfoCard tag="div" title="成功数量" density="compact">
            <p class="text-2xl font-bold tabular-nums text-green-600">{{ status?.success ?? 0 }}</p>
          </InfoCard>
          <InfoCard tag="div" title="失败数量" density="compact">
            <p class="text-2xl font-bold tabular-nums text-red-500">{{ status?.fail ?? 0 }}</p>
          </InfoCard>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="primary" :disabled="startDisabled" @click="startRegistration">
            开始注册
          </Button>
          <Button size="sm" variant="destructive" :disabled="stopDisabled" @click="stopRegistration">
            停止任务
          </Button>
          <Button size="sm" variant="outline" :disabled="!isDirty" @click="saveConfig">
            保存配置
          </Button>
          <span v-if="status" class="ml-auto text-xs text-muted-foreground">
            待恢复 {{ status.pending }} · 不确定 {{ status.uncertain }} · 警告 {{ status.warnings }}
          </span>
        </div>

        <ConsoleSegmentedTabs v-model="activeTab" :options="tabOptions" fit="content" aria-label="配置分区" />

        <FormSection v-if="activeTab === 'basic'" density="roomy" surface="background">
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="block text-xs">
              <span class="ui-field-label">注册数量</span>
              <Input
                :model-value="config['register_count']"
                type="number"
                min="1"
                max="2500"
                block
                @update:model-value="patchConfig('register_count', $event)"
              />
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">多线程 Worker 数</span>
              <Input
                :model-value="config['multi_thread_workers']"
                type="number"
                min="1"
                max="8"
                block
                @update:model-value="patchConfig('multi_thread_workers', $event)"
              />
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['multi_thread_enabled']" @change="patchConfig('multi_thread_enabled', ($event.target as HTMLInputElement).checked)" />
              启用多线程注册
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['enable_nsfw']" @change="patchConfig('enable_nsfw', ($event.target as HTMLInputElement).checked)" />
              启用 NSFW（后处理开关）
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['sso_risk_gate_enabled']" @change="patchConfig('sso_risk_gate_enabled', ($event.target as HTMLInputElement).checked)" />
              SSO 风控早停
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">风控隔离文件</span>
              <Input :model-value="config['sso_risk_rejected_file']" block @update:model-value="patchConfig('sso_risk_rejected_file', $event)" />
            </label>
            <label class="block text-xs md:col-span-2">
              <span class="ui-field-label">代理地址（可选）</span>
              <Input :model-value="config['proxy']" block placeholder="留空表示直连" @update:model-value="patchConfig('proxy', $event)" />
            </label>
            <label class="block text-xs md:col-span-2">
              <span class="ui-field-label">自定义 User-Agent（可选）</span>
              <Input :model-value="config['user_agent']" block placeholder="留空使用默认 UA" @update:model-value="patchConfig('user_agent', $event)" />
            </label>
          </div>
        </FormSection>

        <FormSection v-if="activeTab === 'mail'" density="roomy" surface="background">
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="block text-xs">
              <span class="ui-field-label">邮箱服务商</span>
              <select
                class="ui-select"
                :value="String(config['email_provider'] ?? '')"
                @change="patchConfig('email_provider', ($event.target as HTMLSelectElement).value)"
              >
                <option value="duckmail">duckmail</option>
                <option value="yyds">yyds</option>
                <option value="cloudflare">cloudflare</option>
                <option value="cloudmail">cloudmail</option>
                <option value="outlook">outlook</option>
              </select>
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">Outlook 邮箱池文件</span>
              <Input :model-value="config['outlook_accounts_file']" block @update:model-value="patchConfig('outlook_accounts_file', $event)" />
            </label>

            <div class="space-y-2 border-t border-border pt-3">
              <p class="text-xs font-medium text-muted-foreground">DuckMail</p>
              <label class="block text-xs">
                <span class="ui-field-label">DuckMail API Key</span>
                <Input :model-value="config['duckmail_api_key']" type="password" block @update:model-value="patchConfig('duckmail_api_key', $event)" />
              </label>
            </div>

            <div class="space-y-2 border-t border-border pt-3">
              <p class="text-xs font-medium text-muted-foreground">YYDS</p>
              <label class="block text-xs">
                <span class="ui-field-label">YYDS API Key</span>
                <Input :model-value="config['yyds_api_key']" type="password" block @update:model-value="patchConfig('yyds_api_key', $event)" />
              </label>
              <label class="block text-xs">
                <span class="ui-field-label">YYDS JWT</span>
                <Input :model-value="config['yyds_jwt']" type="password" block @update:model-value="patchConfig('yyds_jwt', $event)" />
              </label>
            </div>

            <div class="space-y-2 md:col-span-2 border-t border-border pt-3">
              <p class="text-xs font-medium text-muted-foreground">Cloudflare</p>
              <label class="block text-xs">
                <span class="ui-field-label">Cloudflare API Base</span>
                <Input :model-value="config['cloudflare_api_base']" block @update:model-value="patchConfig('cloudflare_api_base', $event)" />
              </label>
              <label class="block text-xs">
                <span class="ui-field-label">Cloudflare API Key / Admin Password</span>
                <Input :model-value="config['cloudflare_api_key']" type="password" block @update:model-value="patchConfig('cloudflare_api_key', $event)" />
              </label>
              <label class="block text-xs">
                <span class="ui-field-label">Cloudflare 鉴权模式</span>
                <select
                  class="ui-select"
                  :value="String(config['cloudflare_auth_mode'] ?? '')"
                  @change="patchConfig('cloudflare_auth_mode', ($event.target as HTMLSelectElement).value)"
                >
                  <option value="none">none</option>
                  <option value="bearer">bearer</option>
                  <option value="x-api-key">x-api-key</option>
                  <option value="x-admin-auth">x-admin-auth</option>
                  <option value="query-key">query-key</option>
                </select>
              </label>
              <label class="block text-xs">
                <span class="ui-field-label">Cloudflare 域名路径</span>
                <Input :model-value="config['cloudflare_path_domains']" block @update:model-value="patchConfig('cloudflare_path_domains', $event)" />
              </label>
            </div>

            <div class="space-y-2 md:col-span-2 border-t border-border pt-3">
              <p class="text-xs font-medium text-muted-foreground">Cloud Mail</p>
              <label class="block text-xs">
                <span class="ui-field-label">Cloud Mail API Base</span>
                <Input :model-value="config['cloudmail_api_base']" block @update:model-value="patchConfig('cloudmail_api_base', $event)" />
              </label>
              <label class="block text-xs">
                <span class="ui-field-label">Cloud Mail Public Token</span>
                <Input :model-value="config['cloudmail_public_token']" type="password" block @update:model-value="patchConfig('cloudmail_public_token', $event)" />
              </label>
              <label class="block text-xs">
                <span class="ui-field-label">Cloud Mail 域名</span>
                <Input :model-value="config['cloudmail_domains']" block @update:model-value="patchConfig('cloudmail_domains', $event)" />
              </label>
            </div>
          </div>
        </FormSection>

        <FormSection v-if="activeTab === 'pool'" density="roomy" surface="background">
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['grok2api_auto_add_local']" @change="patchConfig('grok2api_auto_add_local', ($event.target as HTMLInputElement).checked)" />
              自动写入本地池
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">本地 Token 文件</span>
              <Input :model-value="config['grok2api_local_token_file']" block @update:model-value="patchConfig('grok2api_local_token_file', $event)" />
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">Token 池</span>
              <select
                class="ui-select"
                :value="String(config['grok2api_pool_name'] ?? 'ssoBasic')"
                @change="patchConfig('grok2api_pool_name', ($event.target as HTMLSelectElement).value)"
              >
                <option value="ssoBasic">ssoBasic</option>
                <option value="ssoSuper">ssoSuper</option>
              </select>
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['grok2api_auto_add_remote']" @change="patchConfig('grok2api_auto_add_remote', ($event.target as HTMLInputElement).checked)" />
              自动写入远端池
            </label>
            <label class="block text-xs md:col-span-2">
              <span class="ui-field-label">远端 Base URL</span>
              <Input :model-value="config['grok2api_remote_base']" block @update:model-value="patchConfig('grok2api_remote_base', $event)" />
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">旧版 App Key</span>
              <Input :model-value="config['grok2api_remote_app_key']" type="password" block @update:model-value="patchConfig('grok2api_remote_app_key', $event)" />
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">新版管理员账号</span>
              <Input :model-value="config['grok2api_remote_admin_username']" block @update:model-value="patchConfig('grok2api_remote_admin_username', $event)" />
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">新版管理员密码</span>
              <Input :model-value="config['grok2api_remote_admin_password']" type="password" block @update:model-value="patchConfig('grok2api_remote_admin_password', $event)" />
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['grok2api_allow_legacy_full_save']" @change="patchConfig('grok2api_allow_legacy_full_save', ($event.target as HTMLInputElement).checked)" />
              允许旧版全量保存回退
            </label>
          </div>
        </FormSection>

        <FormSection v-if="activeTab === 'cpa'" density="roomy" surface="background">
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['cpa_export_enabled']" @change="patchConfig('cpa_export_enabled', ($event.target as HTMLInputElement).checked)" />
              启用 CPA/OIDC 导出
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">CPA 输出目录</span>
              <Input :model-value="config['cpa_auth_dir']" block @update:model-value="patchConfig('cpa_auth_dir', $event)" />
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['cpa_copy_to_hotload']" @change="patchConfig('cpa_copy_to_hotload', ($event.target as HTMLInputElement).checked)" />
              复制到热加载目录
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">热加载目录</span>
              <Input :model-value="config['cpa_hotload_dir']" block @update:model-value="patchConfig('cpa_hotload_dir', $event)" />
            </label>
            <label class="block text-xs md:col-span-2">
              <span class="ui-field-label">CPA Base URL</span>
              <Input :model-value="config['cpa_base_url']" block @update:model-value="patchConfig('cpa_base_url', $event)" />
            </label>
            <label class="block text-xs md:col-span-2">
              <span class="ui-field-label">CPA 专用代理</span>
              <Input :model-value="config['cpa_proxy']" block @update:model-value="patchConfig('cpa_proxy', $event)" />
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['cpa_headless']" @change="patchConfig('cpa_headless', ($event.target as HTMLInputElement).checked)" />
              CPA 无头模式
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['cpa_force_standalone']" @change="patchConfig('cpa_force_standalone', ($event.target as HTMLInputElement).checked)" />
              CPA 独立浏览器
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">CPA 授权整体超时（秒）</span>
              <Input :model-value="config['cpa_mint_timeout_sec']" type="number" min="30" max="1800" block @update:model-value="patchConfig('cpa_mint_timeout_sec', $event)" />
            </label>
            <label class="flex items-center gap-2 text-xs">
              <input type="checkbox" :checked="!!config['cpa_mint_cookie_inject']" @change="patchConfig('cpa_mint_cookie_inject', ($event.target as HTMLInputElement).checked)" />
              注入已有 Cookie
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">OIDC 请求超时（秒）</span>
              <Input :model-value="config['cpa_oidc_request_timeout_sec']" type="number" min="3" max="120" block @update:model-value="patchConfig('cpa_oidc_request_timeout_sec', $event)" />
            </label>
            <label class="block text-xs">
              <span class="ui-field-label">OIDC 轮询超时（秒）</span>
              <Input :model-value="config['cpa_oidc_poll_timeout_sec']" type="number" min="3" max="120" block @update:model-value="patchConfig('cpa_oidc_poll_timeout_sec', $event)" />
            </label>
            <label class="block text-xs md:col-span-2">
              <span class="ui-field-label">外部 cpa_xai 工具目录</span>
              <Input :model-value="config['api_reverse_tools']" block @update:model-value="patchConfig('api_reverse_tools', $event)" />
            </label>
          </div>
        </FormSection>

        <FormSection title="实时控制台日志" density="compact" surface="background">
          <template #actions>
            <Button size="xs" variant="outline" @click="copyLogs">复制日志</Button>
            <Button size="xs" variant="outline" @click="clearLogs">清空</Button>
          </template>
          <div ref="logRef" class="log-terminal">
            <p v-if="logs.length === 0" class="text-xs text-muted-foreground">[*] 控制台已就绪，等待指令...</p>
            <p v-for="entry in logs" :key="entry.seq" :class="`log-line log-line--${entry.tone}`">{{ entry.line }}</p>
          </div>
        </FormSection>

        <FormSection title="代理池状态" density="compact" surface="background">
          <template #actions>
            <Button size="xs" variant="outline" :disabled="proxyPoolLoading" @click="loadProxyPool">刷新</Button>
            <Button size="xs" variant="primary" :disabled="proxyPoolLoading" @click="reloadProxyPool">重新加载</Button>
          </template>
          <div v-if="proxyPool">
            <p class="text-xs text-muted-foreground">
              模式 <strong class="text-foreground">{{ proxyPool.mode }}</strong>
              · 节点 <strong class="text-foreground">{{ proxyPool.nodes.length }}</strong>
              · 容量 <strong class="text-foreground">{{ proxyPool.capacity }}</strong>
            </p>
            <TableShell v-if="proxyPool.nodes.length > 0" class="mt-2">
              <table class="w-full text-xs">
                <thead>
                  <tr class="text-left text-muted-foreground">
                    <th class="py-1">代理</th>
                    <th class="py-1">健康</th>
                    <th class="py-1">失败</th>
                    <th class="py-1">延迟</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="node in proxyPool.nodes" :key="node.id">
                    <td class="py-1 font-mono">{{ node.proxy }}</td>
                    <td class="py-1">{{ node.health }}%</td>
                    <td class="py-1">{{ node.failure_count }}</td>
                    <td class="py-1">{{ node.probe_latency_ms }}ms</td>
                  </tr>
                </tbody>
              </table>
            </TableShell>
            <p v-else class="mt-2 text-xs text-muted-foreground">代理池暂无节点。</p>
          </div>
          <p v-else-if="proxyPoolError" class="text-xs text-destructive">{{ proxyPoolError }}</p>
          <p v-else class="text-xs text-muted-foreground">未加载代理池状态，点击刷新查看。</p>
        </FormSection>

        <FormSection title="Outlook 邮箱池" density="compact" surface="background">
          <template #actions>
            <Button size="xs" variant="outline" @click="loadMailboxes">刷新</Button>
            <Button size="xs" variant="primary" :disabled="mailboxSaving" @click="saveMailboxes">保存</Button>
            <Button size="xs" variant="outline" @click="testMailboxes">测试</Button>
          </template>
          <label class="block text-xs">
            <span class="ui-field-label">邮箱列表（每行一个 email@domain 或 email@domain:password）</span>
            <textarea
              v-model="mailboxDraft"
              class="ui-textarea mt-1 min-h-[100px]"
            />
          </label>
          <p v-if="mailboxTest" class="mt-2 text-xs text-muted-foreground">
            共 {{ mailboxTest.count }} 个邮箱，{{ mailboxTest.healthy }} 个可用。
          </p>
          <p v-if="mailboxPool" class="mt-1 text-xs text-muted-foreground">
            当前池文件 <span class="font-mono">{{ mailboxPool.path }}</span>，共 {{ mailboxPool.count }} 个邮箱。
          </p>
        </FormSection>
      </template>
    </PagePanel>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { Button, Input, StatusPill, Checkbox } from 'nanocat-ui'
import {
  ConsoleSegmentedTabs,
  FormSection,
  InfoCard,
  PageLoadingState,
  PagePanel,
  PanelHeader,
  TableShell,
} from '@/components/ai'
import type { SegmentedOption } from 'nanocat-ui'
import { useGrokRegister } from '@/composables/useGrokRegister'

const {
  isConnected,
  isConnecting,
  isRunning,
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
} = useGrokRegister()

const activeTab = ref('basic')
const logRef = ref<HTMLDivElement | null>(null)

const tabOptions: SegmentedOption[] = [
  { label: '任务参数', value: 'basic' },
  { label: '邮箱服务', value: 'mail' },
  { label: 'grok2api 池', value: 'pool' },
  { label: 'CPA 导出', value: 'cpa' },
]

// 切到对应标签时按需拉取扩展数据，避免首屏多余请求
watch(activeTab, (tab) => {
  if (tab === 'pool') void loadProxyPool()
  if (tab === 'mail') void loadMailboxes()
})

// 新日志追加后自动滚到底部，贴近原 WebUI 终端体验
watch(
  () => logs.value.length,
  () => {
    requestAnimationFrame(() => {
      if (!logRef.value) return
      logRef.value.scrollTop = logRef.value.scrollHeight
    })
  },
)
</script>

<style scoped>
.ui-select {
  width: 100%;
  height: 2.25rem;
  border-radius: 0.375rem;
  border: 1px solid hsl(var(--border));
  background: hsl(var(--background));
  padding: 0 0.75rem;
  font-size: 12px;
  color: hsl(var(--foreground));
  outline: none;
}

.ui-select:focus {
  border-color: hsl(var(--primary) / 0.6);
  box-shadow: 0 0 0 2px hsl(var(--primary) / 0.15);
}

.ui-textarea {
  width: 100%;
  border-radius: 0.375rem;
  border: 1px solid hsl(var(--border));
  background: hsl(var(--background));
  padding: 0.5rem;
  font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  color: hsl(var(--foreground));
  outline: none;
}

.ui-textarea:focus {
  border-color: hsl(var(--primary) / 0.6);
}

.log-terminal {
  max-height: 320px;
  overflow-y: auto;
  border-radius: 0.5rem;
  border: 1px solid hsl(var(--border));
  background: hsl(var(--muted) / 0.3);
  padding: 12px 14px;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  line-height: 1.75;
}

.log-line {
  margin-bottom: 3px;
  white-space: pre-wrap;
  word-break: break-word;
}

.log-line--ok { color: #6ee7a1; }
.log-line--bad { color: #ff7f7f; }
.log-line--info { color: #17c7ff; }
.log-line--warn { color: #ffd178; }
.log-line--plain { color: hsl(var(--muted-foreground)); }
</style>
