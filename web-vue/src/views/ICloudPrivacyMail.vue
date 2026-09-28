<template>
  <div class="space-y-6">
    <PagePanel class="space-y-5">
      <PanelHeader title="iCloud 邮箱" align="start">
        <template #copy>
          <p class="mt-1 text-xs text-muted-foreground">
            独立管理 Apple 登录态、隐私邮箱和验证码邮件。密码与 2FA 只在当前请求中使用。
          </p>
        </template>
        <template #actions>
          <StateBadge :tone="bridgeTone" shape="rounded">
            {{ bridgeLabel }}
          </StateBadge>
          <Button size="sm" variant="outline" :disabled="loading" @click="refreshAll">
            <Icon icon="lucide:refresh-cw" class="h-3.5 w-3.5" :class="loading ? 'animate-spin' : ''" />
            {{ loading ? '刷新中...' : '刷新' }}
          </Button>
        </template>
      </PanelHeader>

      <!-- Bridge offline with retry -->
      <div v-if="bridge && !bridge.reachable" class="rounded-lg border border-amber-300/70 bg-amber-50/70 p-4 dark:border-amber-800/60 dark:bg-amber-950/20">
        <div class="flex items-start justify-between gap-3">
          <div class="flex-1">
            <p class="text-sm font-semibold text-amber-900 dark:text-amber-200">iCloud 模块离线</p>
            <p class="mt-1 text-xs leading-5 text-amber-800 dark:text-amber-300">
              请确认 sidecar 已启动：<code class="rounded bg-amber-100 px-1 py-0.5 text-[10px] font-mono dark:bg-amber-900/50">ICLOUD_PRIVACY_MAIL_BASE_URL</code>
              环境变量配置正确，且服务运行在对应端口。
            </p>
            <div v-if="bridge.base_url" class="mt-2 flex flex-wrap items-center gap-2 text-xs">
              <span class="text-muted-foreground">当前地址:</span>
              <code class="rounded bg-amber-100 px-2 py-1 font-mono text-[10px] dark:bg-amber-900/50">{{ bridge.base_url }}</code>
              <Button size="xs" variant="outline" @click="retryBridgeCheck">
                <Icon icon="lucide:refresh-cw" class="h-3 w-3" />
                重试检测
              </Button>
            </div>
          </div>
        </div>
      </div>

      <StateBlock v-if="pageError" compact dashed :title="pageErrorTitle" :description="pageError" />

      <template v-if="bridge?.reachable">
        <!-- Process Steps Indicator -->
        <ProcessSteps :steps="processSteps" @step-click="handleStepClick" />
        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div v-for="metric in metrics" :key="metric.label" class="rounded-lg border border-border bg-muted/20 px-3 py-3">
            <p class="text-xs text-muted-foreground">{{ metric.label }}</p>
            <p class="mt-1 text-xl font-semibold tabular-nums text-foreground">{{ metric.value }}</p>
          </div>
        </div>

        <div class="grid gap-5 xl:grid-cols-[minmax(0,1.1fr)_minmax(22rem,.9fr)]">
          <FormSection title="Apple 登录态" density="roomy">
            <template #actions>
              <Button size="xs" variant="outline" :disabled="appleBusy || sessions.length === 0" @click="checkSessions">检测状态</Button>
            </template>
            <div class="space-y-3">
              <div v-if="sessions.length" class="space-y-2">
                <label v-for="session in sessions" :key="session.account_id || session.apple_id" class="flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 transition-colors" :class="activeMailboxAccountId === session.account_id ? 'border-sky-400 bg-sky-50/60 dark:border-sky-700 dark:bg-sky-950/20' : 'border-border'" @click="selectMailboxAccount(session.account_id)">
                  <input v-model="selectedAccountIds" type="checkbox" :value="session.account_id" class="mt-1 h-4 w-4 accent-sky-600" :disabled="!session.account_id" @click.stop @change="handleAccountSelectionChange(session.account_id)" />
                  <span class="min-w-0 flex-1">
                    <span class="flex items-center gap-2"><span class="min-w-0 truncate text-sm font-medium text-foreground">{{ session.apple_id || session.account_label || 'Apple 账号' }}</span><MetaChip v-if="activeMailboxAccountId === session.account_id" size="xs" tone="info">当前邮箱</MetaChip></span>
                    <span class="mt-1 flex flex-wrap gap-1.5 text-[11px]">
                      <MetaChip v-if="session.apple_account_login_saved" size="xs" :tone="loginStateTone(session.apple_account_login_checked, session.apple_account_login_ok)" :title="session.apple_account_login_status">新接口 {{ loginStateLabel(session.apple_account_login_checked, session.apple_account_login_ok) }}</MetaChip>
                      <MetaChip v-if="session.icloud_web_login_saved" size="xs" :title="session.icloud_web_login_status" :tone="loginStateTone(session.icloud_web_login_checked, session.icloud_web_login_ok)">旧接口 {{ loginStateLabel(session.icloud_web_login_checked, session.icloud_web_login_ok) }}</MetaChip>
                      <MetaChip v-if="session.icloud_imap_login_saved" size="xs" :tone="loginStateTone(session.icloud_imap_login_checked, session.icloud_imap_login_ok)" :title="session.icloud_imap_login_status">取码 {{ loginStateLabel(session.icloud_imap_login_checked, session.icloud_imap_login_ok) }}</MetaChip>
                    </span>
                  </span>
                </label>
              </div>
              <p v-else class="rounded-lg border border-dashed border-border px-3 py-4 text-xs text-muted-foreground">还没有 Apple 登录态，先在右侧发起登录。</p>
              <div class="border-t border-border pt-3">
                <p class="mb-2 text-xs font-medium text-foreground">添加 Apple 登录态</p>
                <div class="grid gap-2 sm:grid-cols-2">
                  <label class="text-xs"><span class="ui-field-label">登录通道</span><select v-model="appleForm.channel" class="ui-select"><option value="apple">新接口：Apple Account</option><option value="icloud">旧接口：iCloud Web</option></select></label>
                  <label class="text-xs"><span class="ui-field-label">验证码方式</span><select v-model="appleForm.two_factor_method" class="ui-select"><option value="trusted_device">受信任设备</option><option value="sms">短信</option></select></label>
                  <label class="text-xs"><span class="ui-field-label">Apple ID</span><Input v-model="appleForm.apple_id" block autocomplete="username" placeholder="name@example.com" /></label>
                  <label class="text-xs"><span class="ui-field-label">Apple 密码</span><Input v-model="appleForm.password" block type="password" autocomplete="current-password" placeholder="不会保存到主系统" /></label>
                </div>
                <div class="rounded-lg border border-sky-200/80 bg-sky-50/60 px-3 py-2.5 text-xs leading-5 text-sky-900 dark:border-sky-900/60 dark:bg-sky-950/20 dark:text-sky-200">
                  <p v-if="appleForm.channel === 'apple'"><strong>新接口用途：</strong>Apple Account 管理接口，用于创建隐私邮箱。</p>
                  <p v-else><strong>旧接口用途：</strong>iCloud Web / Hide My Email 接口，用于同步已有隐私邮箱。</p>
                  <p class="mt-1"><strong>App 专用密码用途：</strong>仅用于通过 iCloud IMAP 接收验证码邮件，不参与 Apple 登录或隐私邮箱创建。</p>
                </div>
                <div v-if="applePending" class="mt-3 rounded-lg border border-amber-300/60 bg-amber-50/70 p-3 dark:bg-amber-950/20">
                  <p class="text-xs font-medium text-foreground">Apple 要求 2FA：{{ applePending.message || '请输入本次收到的 6 位验证码' }}</p>
                  <div v-if="appleForm.pending_id && pageError && /Apple (?:协议|服务|登录)/i.test(pageError)" class="mt-2 rounded-md border border-amber-200/80 bg-yellow-50/50 p-2 text-[11px] leading-5 text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-200">
                    <p class="font-semibold">可能的原因与建议</p>
                    <p v-if="appleErrorHint(pageError)" class="mt-1">{{ appleErrorHint(pageError) }}</p>
                    <p v-else class="mt-1">请检查 Apple ID 是否正确，验证码是否已过期或输入错误。如仍失败，请先在 Apple 设备上确认信任请求。</p>
                  </div>
                  <div class="mt-2 flex flex-wrap items-end gap-2">
                    <label class="text-xs"><span class="ui-field-label">验证码</span><Input v-model="appleForm.code" block inputmode="numeric" maxlength="6" placeholder="000000" /></label>
                    <Button size="sm" variant="primary" :disabled="appleBusy || appleForm.code.length < 6" @click="submitApple2fa">{{ appleBusy ? '验证中...' : '提交 2FA' }}</Button>
                  </div>
                </div>
                <Button v-else class="mt-3" size="sm" variant="primary" :disabled="appleBusy || !appleForm.apple_id || !appleForm.password" @click="startAppleLogin">{{ appleBusy ? '连接 Apple...' : '开始登录并等待 2FA' }}</Button>
              </div>
            </div>
          </FormSection>

          <div class="space-y-5">
            <FormSection title="取码登录" density="roomy">
              <p class="mb-3 text-xs text-muted-foreground">验证码通过 iCloud IMAP 接收，需要先在 Apple 账户生成 App 专用密码。</p>
              <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                <label v-if="sessions.length" class="text-xs"><span class="ui-field-label">绑定 Apple 账号</span><select v-model="imapForm.account_id" class="ui-select"><option value="">自动匹配</option><option v-for="session in sessions" :key="session.account_id || session.apple_id" :value="session.account_id">{{ session.apple_id || session.account_label || session.account_id }}</option></select></label>
                <label class="text-xs"><span class="ui-field-label">iCloud 邮箱</span><Input v-model="imapForm.email" block placeholder="name@icloud.com" /></label>
                <label class="text-xs"><span class="ui-field-label">App 专用密码</span><Input v-model="imapForm.app_password" block type="password" placeholder="xxxx-xxxx-xxxx-xxxx" /></label>
              </div>
              <div class="mt-3 flex flex-wrap gap-2">
                <Button size="sm" variant="primary" :disabled="imapBusy || !imapForm.email || !imapForm.app_password" @click="saveImap">{{ imapBusy ? '保存中...' : '保存并检测' }}</Button>
                <Button size="sm" variant="outline" :disabled="imapBusy" @click="checkImap">重新检测</Button>
              </div>
            </FormSection>

            <FormSection title="创建隐私邮箱" density="roomy">
              <div class="grid gap-3 sm:grid-cols-2">
                <label class="text-xs"><span class="ui-field-label">邮箱标签</span><Input v-model="createForm.label" block placeholder="注册批次 0715" /></label>
                <label class="text-xs"><span class="ui-field-label">创建通道</span><select v-model="createForm.create_channel" class="ui-select"><option value="">自动选择</option><option value="apple_account">新接口</option><option value="icloud_web">旧接口</option></select></label>
                <label class="text-xs sm:col-span-2"><span class="ui-field-label">备注</span><Input v-model="createForm.note" block placeholder="可选" /></label>
              </div>
              <div class="mt-3 flex items-center justify-between gap-3">
                <p class="text-xs text-muted-foreground">已选择 {{ selectedAccountIds.length }} 个 Apple 账号</p>
                <Button size="sm" variant="primary" :disabled="createBusy || selectedAccountIds.length === 0" @click="createMailboxes">{{ createBusy ? '创建中...' : '创建邮箱' }}</Button>
              </div>
            </FormSection>

            <FormSection title="定时创建" density="roomy">
              <template #actions>
                <MetaChip size="xs" :tone="schedulerTone">{{ schedulerLabel }}</MetaChip>
              </template>
              <div class="space-y-3">
                <p class="text-xs leading-5 text-muted-foreground">按当前勾选的 Apple 账号循环创建。每 60 分钟执行一批：新接口最多 {{ scheduler?.apple_account_quota || 20 }} 个、旧接口最多 {{ scheduler?.icloud_web_quota || 5 }} 个；每个账号达到 {{ scheduler?.target_per_account || 750 }} 个后自动停止。</p>
                <div class="grid gap-3 sm:grid-cols-2">
                  <label class="text-xs"><span class="ui-field-label">创建通道</span><select v-model="schedulerForm.create_channel" class="ui-select" :disabled="schedulerBusy || Boolean(scheduler?.running)"><option value="">自动使用新旧接口</option><option value="apple_account">只用新接口（约 20/小时）</option><option value="icloud_web">只用旧接口（约 5/小时）</option></select></label>
                  <label class="text-xs"><span class="ui-field-label">执行间隔（分钟）</span><Input v-model.number="schedulerForm.interval_minutes" block type="number" min="60" step="60" :disabled="schedulerBusy || Boolean(scheduler?.running)" /></label>
                </div>
                <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span>参与账号：{{ selectedAccountIds.length }}</span>
                  <span>每账号目标：{{ scheduler?.target_per_account || 750 }} 个</span>
                  <span>当前成功：{{ scheduler?.success || 0 }}</span>
                  <span>当前失败：{{ scheduler?.failed || 0 }}</span>
                </div>
                <div class="flex flex-wrap gap-2">
                  <Button size="sm" variant="primary" :disabled="schedulerBusy || Boolean(scheduler?.running) || selectedAccountIds.length === 0" @click="startScheduler"><Icon icon="lucide:play" class="h-3.5 w-3.5" />{{ schedulerBusy ? '启动中...' : '启动定时创建' }}</Button>
                  <Button size="sm" variant="outline" :disabled="schedulerBusy || !scheduler?.running" @click="stopScheduler"><Icon icon="lucide:square" class="h-3.5 w-3.5" />停止</Button>
                  <Button size="sm" variant="ghost" title="清除定时创建日志" :disabled="schedulerBusy || !(scheduler?.events || []).length" @click="clearSchedulerLogs"><Icon icon="lucide:trash-2" class="h-3.5 w-3.5" />清除日志</Button>
                </div>
                <div v-if="scheduler?.running || scheduler?.next_run_at || scheduler?.last_error" class="space-y-1 rounded-lg border border-border bg-muted/20 px-3 py-2 text-xs">
                  <p v-if="scheduler?.next_run_at" class="text-muted-foreground">下一次执行：{{ formatDate(scheduler.next_run_at) }}</p>
                  <p v-if="scheduler?.last_error" class="break-words text-amber-700 dark:text-amber-300">最近错误：{{ scheduler.last_error }}</p>
                  <p v-if="scheduler?.status" class="text-muted-foreground">状态：{{ scheduler.status }}</p>
                </div>
                <div v-if="schedulerEvents.length" class="max-h-32 space-y-1 overflow-auto border-t border-border pt-2">
                  <p v-for="event in schedulerEvents.slice(0, 5)" :key="event.id" class="break-words text-[11px] text-muted-foreground">{{ formatDate(event.at) }} · {{ event.message || event.type || '-' }}<span v-if="event.email"> · {{ event.email }}</span></p>
                </div>
              </div>
            </FormSection>
          </div>
        </div>

        <FormSection title="隐私邮箱与取件" density="roomy">
          <template #actions>
            <Button size="xs" variant="outline" :disabled="mailboxBusy" @click="syncAllMailboxes">{{ mailboxBusy ? '同步中...' : '同步邮箱' }}</Button>
          </template>
          <div class="mb-4 flex flex-wrap items-end justify-between gap-3 border-b border-border pb-3">
            <div class="flex min-w-0 flex-1 items-end gap-2 sm:max-w-md">
              <label class="min-w-0 flex-1 text-xs"><span class="ui-field-label">搜索邮箱</span><Input v-model="mailboxSearch" block placeholder="邮箱地址或标签" @keyup.enter="searchMailboxes" /></label>
              <Button size="sm" variant="outline" title="搜索邮箱" @click="searchMailboxes"><Icon icon="lucide:search" class="h-3.5 w-3.5" /></Button>
            </div>
            <div class="flex items-center gap-2 text-xs text-muted-foreground">
              <span class="tabular-nums">第 {{ mailboxPage }} / {{ mailboxTotalPages }} 页</span>
              <Button size="xs" variant="outline" title="上一页" :disabled="mailboxPage <= 1" @click="changeMailboxPage(-1)"><Icon icon="lucide:chevron-left" class="h-3.5 w-3.5" /></Button>
              <Button size="xs" variant="outline" title="下一页" :disabled="mailboxPage >= mailboxTotalPages" @click="changeMailboxPage(1)"><Icon icon="lucide:chevron-right" class="h-3.5 w-3.5" /></Button>
            </div>
          </div>
          <div v-if="syncFailures.length" class="mb-4 rounded-lg border border-amber-300/70 bg-amber-50/70 px-3 py-3 dark:border-amber-800/60 dark:bg-amber-950/20">
            <div class="flex items-start justify-between gap-3">
              <p class="text-xs font-semibold text-amber-900 dark:text-amber-200">部分 Apple 账号同步失败</p>
              <Button size="xs" variant="outline" :disabled="mailboxBusy" @click="retrySyncFailed">
                <Icon icon="lucide:refresh-cw" class="h-3 w-3" />
                {{ mailboxBusy ? '重试中...' : '重试全部' }}
              </Button>
            </div>
            <div class="mt-2 space-y-2">
              <div v-for="failure in syncFailures" :key="failure.account_id || failure.apple_id || failure.error" class="rounded-md border border-amber-200/80 bg-background/70 px-2.5 py-2 dark:border-amber-900/60">
                <p class="text-xs font-medium text-foreground">{{ failure.apple_id || failure.account_id || '未知 Apple 账号' }}</p>
                <p class="mt-1 break-words text-xs text-amber-800 dark:text-amber-200">{{ failure.error || '服务未返回具体原因' }}</p>
              </div>
            </div>
            <p v-if="syncFailures.some(needsOldICloudLogin)" class="mt-3 text-xs leading-5 text-amber-900 dark:text-amber-200">
              如果提示旧接口或 iCloud Web 登录缺失，请在上方登录通道选择“旧接口：iCloud Web”完成 Apple 登录；新接口主要用于创建邮箱。
            </p>
          </div>
          <div v-if="mailboxes.length" class="grid min-w-0 gap-3 lg:grid-cols-2">
            <div v-for="mailbox in mailboxes" :key="mailbox.id" :data-mailbox-id="mailbox.id" class="min-w-0 rounded-lg border border-border bg-background p-3">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <p class="truncate font-mono text-sm font-medium text-foreground">{{ mailbox.email || '-' }}</p>
                  <p class="mt-1 truncate text-xs text-muted-foreground">{{ mailbox.label || '未命名' }} · {{ mailbox.account_label || mailbox.account_apple_id || '未绑定账号' }}</p>
                </div>
                <div class="flex flex-wrap items-center justify-end gap-1.5">
                  <MetaChip v-if="mailbox.openai_claimed" size="xs" tone="success">GPT 已注册</MetaChip>
                  <MetaChip v-if="mailbox.grok_claimed" size="xs" tone="info">Grok 已注册</MetaChip>
                  <MetaChip size="xs" :tone="mailboxAvailabilityTone(mailbox)">{{ mailboxAvailabilityLabel(mailbox) }}</MetaChip>
                </div>
              </div>
              <div class="mt-3 flex flex-wrap gap-2">
                <Button data-action="show-api" size="xs" variant="outline" :disabled="!mailbox.api_url" :title="mailbox.api_url || '暂无 API 地址'" @click="showMailboxApi(mailbox)"><Icon icon="lucide:link" class="h-3.5 w-3.5" />API 地址</Button>
                <Button data-action="copy-email" size="xs" variant="outline" :disabled="!mailbox.email" @click="copyMailboxEmail(mailbox)"><Icon icon="lucide:copy" class="h-3.5 w-3.5" />复制邮箱</Button>
                <Button data-action="copy-api" size="xs" variant="outline" :disabled="!mailbox.api_url" @click="copyMailboxApi(mailbox)"><Icon icon="lucide:copy" class="h-3.5 w-3.5" />复制 API</Button>
                <Button data-action="copy-pair" size="xs" variant="outline" :disabled="!mailbox.email || !mailbox.api_url" @click="copyMailboxPair(mailbox)"><Icon icon="lucide:clipboard" class="h-3.5 w-3.5" />邮箱+API</Button>
                <Button size="xs" variant="outline" :disabled="mailboxBusy || !mailbox.id" @click="syncMailbox(mailbox)"><Icon icon="lucide:download" class="h-3.5 w-3.5" />同步邮件</Button>
                <Button size="xs" variant="outline" :disabled="codeBusyId === mailbox.id || !mailbox.id" @click="fetchCode(mailbox)"><Icon icon="lucide:key-round" class="h-3.5 w-3.5" />{{ codeBusyId === mailbox.id ? '取码中...' : '取验证码' }}</Button>
                <Button size="xs" variant="ghost" :disabled="messageBusyId === mailbox.id || !mailbox.id" @click="toggleMessages(mailbox)">{{ messageBusyId === mailbox.id ? '加载中...' : expandedMailboxId === mailbox.id ? '收起邮件' : '查看邮件' }}</Button>
              </div>
              <div v-if="codeByMailbox[mailbox.id || '']" class="mt-3 flex items-center justify-between rounded-md bg-emerald-50 px-3 py-2 dark:bg-emerald-950/30">
                <div>
                  <p class="text-xs text-emerald-700 dark:text-emerald-300">最新验证码 · {{ mailbox.email }}</p>
                  <strong class="font-mono text-lg tracking-[0.2em] text-emerald-800 dark:text-emerald-200">{{ codeByMailbox[mailbox.id || ''] }}</strong>
                </div>
                <Button size="xs" variant="outline" @click="copyText(codeByMailbox[mailbox.id || ''], '验证码已复制')">
                  <Icon icon="lucide:copy" class="h-3 w-3" />
                  复制
                </Button>
              </div>
              <div v-if="expandedMailboxId === mailbox.id" class="mt-3 space-y-2 border-t border-border pt-3">
                <div v-for="message in messagesByMailbox[mailbox.id || ''] || []" :key="message.id" class="rounded-md bg-muted/30 p-2.5 text-xs">
                  <div class="flex items-start justify-between gap-2"><p class="font-medium text-foreground">{{ message.subject || '(无主题)' }}</p><time class="shrink-0 text-muted-foreground">{{ formatDate(message.received_at) }}</time></div>
                  <p class="mt-1 text-muted-foreground">{{ message.from || '-' }}</p>
                  <p class="mt-2 max-h-28 overflow-auto whitespace-pre-wrap break-words text-foreground/80">{{ message.body || '(无正文)' }}</p>
                </div>
                <p v-if="!(messagesByMailbox[mailbox.id || ''] || []).length" class="text-xs text-muted-foreground">暂无同步邮件</p>
              </div>
            </div>
          </div>
          <p v-else class="rounded-lg border border-dashed border-border px-3 py-5 text-center text-xs text-muted-foreground">暂无隐私邮箱，创建后会在这里显示。</p>
        </FormSection>

        <p v-if="notice" class="break-all rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-xs text-sky-800 dark:border-sky-900/50 dark:bg-sky-950/30 dark:text-sky-200">{{ notice }}</p>
      </template>
      <p v-else class="rounded-lg border border-dashed border-border px-3 py-5 text-center text-xs text-muted-foreground">
        主系统已登录，等待 iCloud 服务连接后即可使用。
      </p>
    </PagePanel>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Icon } from '@iconify/vue'
import { Button, Input } from 'nanocat-ui'
import { icloudApi, type ICloudMailbox, type ICloudMailboxSyncResult, type ICloudMessage, type ICloudScheduler, type ICloudSession } from '@/api/icloud'
import FormSection from '@/components/ai/FormSection.vue'
import MetaChip from '@/components/ai/MetaChip.vue'
import PagePanel from '@/components/ai/PagePanel.vue'
import PanelHeader from '@/components/ai/PanelHeader.vue'
import ProcessSteps from '@/components/ai/ProcessSteps.vue'
import StateBadge from '@/components/ai/StateBadge.vue'
import StateBlock from '@/components/ai/StateBlock.vue'

defineOptions({ name: 'ICloudPrivacyMail' })

const loading = ref(false)
const appleBusy = ref(false)
const imapBusy = ref(false)
const createBusy = ref(false)
const schedulerBusy = ref(false)
const mailboxBusy = ref(false)
const codeBusyId = ref('')
const messageBusyId = ref('')
const expandedMailboxId = ref('')
const pageError = ref('')
const notice = ref('')
const bridge = ref<{ enabled?: boolean; reachable?: boolean; base_url?: string; status_code?: number } | null>(null)
const appleForm = reactive({ channel: 'apple' as 'apple' | 'icloud', two_factor_method: 'trusted_device', apple_id: '', password: '', code: '', pending_id: '' })
const applePending = ref<{ message?: string; expires_at?: string } | null>(null)
const imapForm = reactive({ account_id: '', email: '', app_password: '' })
const createForm = reactive({ label: '', note: '', create_channel: '' })
const schedulerForm = reactive({ create_channel: '', interval_minutes: 60, round_interval_seconds: 5 })
const sessions = ref<ICloudSession[]>([])
const selectedAccountIds = ref<string[]>([])
const activeMailboxAccountId = ref('')
const mailboxes = ref<ICloudMailbox[]>([])
const mailboxPage = ref(1)
const mailboxPageSize = ref(20)
const mailboxTotal = ref(0)
const mailboxTotalPages = ref(1)
const mailboxSearch = ref('')
const syncFailures = ref<ICloudMailboxSyncResult[]>([])
const scheduler = ref<ICloudScheduler | null>(null)
const messagesByMailbox = reactive<Record<string, ICloudMessage[]>>({})
const codeByMailbox = reactive<Record<string, string>>({})
let schedulerTimer: number | undefined

const bridgeTone = computed(() => bridge.value?.reachable ? 'success' : bridge.value ? 'danger' : 'muted')
const bridgeLabel = computed(() => bridge.value?.reachable ? '模块在线' : bridge.value ? '模块离线' : '检查中')
const pageErrorTitle = computed(() => {
  if (pageError.value.includes('未保存 iCloud 登录态')) return '尚未完成 Apple 登录'
  if (/Apple (?:协议|服务|登录)/i.test(pageError.value)) return 'Apple 登录暂时失败'
  return 'iCloud 模块暂不可用'
})
const schedulerTone = computed(() => scheduler.value?.running ? 'success' : scheduler.value?.last_error ? 'danger' : 'muted')
const schedulerLabel = computed(() => scheduler.value?.running ? '运行中' : scheduler.value?.last_error ? '已停止 / 有错误' : '未运行')
const schedulerEvents = computed(() => scheduler.value?.events || [])
const metrics = computed(() => [
  { label: 'Apple 登录态', value: sessions.value.length },
  { label: '可创建账号', value: sessions.value.filter(item => item.apple_account_login_saved || item.icloud_web_login_saved).length },
  { label: '隐私邮箱', value: mailboxTotal.value },
  { label: '当前页可领取', value: mailboxes.value.filter(mailboxCanClaimAny).length },
])

const hasAppleLogin = computed(() => sessions.value.some(session => session.apple_account_login_saved || session.icloud_web_login_saved))
const hasImapLogin = computed(() => sessions.value.some(session => session.icloud_imap_login_saved))

const processSteps = computed(() => {
  const steps = [
    {
      title: '连接 iCloud 模块',
      status: bridge.value?.reachable ? 'completed' : 'active',
      description: bridge.value?.reachable ? 'sidecar 已就绪' : '等待 sidecar 启动',
    },
    {
      title: 'Apple 登录 + 2FA',
      status: hasAppleLogin.value ? 'completed' : applePending.value || appleBusy.value ? 'active' : 'pending',
      description: hasAppleLogin.value ? '登录态已保存' : '发起登录并完成双因素验证',
      action: '去登录',
    },
    {
      title: 'IMAP 取码登录',
      status: hasImapLogin.value ? 'completed' : hasAppleLogin.value ? 'active' : 'pending',
      description: hasImapLogin.value ? 'App 专用密码已配置' : '保存 App 专用密码用于接收验证码',
      action: '配置 IMAP',
    },
    {
      title: '创建隐私邮箱',
      status: mailboxes.value.length > 0 ? 'completed' : hasImapLogin.value ? 'active' : 'pending',
      description: mailboxes.value.length > 0 ? `已有 ${mailboxTotal.value} 个邮箱` : '手动或定时创建 HME 邮箱',
      action: '创建邮箱',
    },
    {
      title: '同步 / 取验证码',
      status: Object.keys(codeByMailbox).length > 0 ? 'completed' : mailboxes.value.length > 0 ? 'active' : 'pending',
      description: '同步邮件并提取验证码',
    },
  ] as { title: string; status: 'pending' | 'active' | 'completed' | 'error'; description?: string; action?: string }[]

  // 同步失败时最后一步标为 error
  if (syncFailures.value.length > 0 && steps[steps.length - 1].status !== 'completed') {
    steps[steps.length - 1] = { ...steps[steps.length - 1], status: 'error', description: `${syncFailures.value.length} 个账号同步失败，点击处理` }
  }

  return steps
})

function handleStepClick(step: { title: string; action?: string }) {
  if (step.title.includes('Apple 登录')) {
    document.querySelector<HTMLInputElement>('input[autocomplete="username"]')?.focus()
    return
  }
  if (step.title.includes('IMAP')) {
    document.querySelector<HTMLInputElement>('input[placeholder="name@icloud.com"]')?.focus()
    return
  }
  if (step.title.includes('创建隐私邮箱') || step.title.includes('同步')) {
    document.querySelector<HTMLElement>('[data-mailbox-section]')?.scrollIntoView({ behavior: 'smooth' })
  }
}

async function retryBridgeCheck() {
  bridge.value = null
  try {
    bridge.value = await icloudApi.bridgeStatus()
  } catch (error) {
    pageError.value = errorText(error)
  }
}

function appleErrorHint(errorText: string): string {
  if (/locked|锁定/i.test(errorText)) return '账号已被 Apple 锁定，请先在 Apple 官网解锁后再试。'
  if (/pending|过期|expired/i.test(errorText)) return '2FA 会话已过期，请重新发起登录。'
  if (/verification|验证码|code/i.test(errorText) && /invalid|incorrect|错误|mismatch/i.test(errorText)) return '验证码不正确，请确认输入的是最新一条 6 位数字，并检查选择的验证码方式（受信任设备 / 短信）是否与实际接收渠道一致。'
  if (/password|密码/i.test(errorText)) return 'Apple ID 或密码错误，或该账号被限制登录。'
  if (/rate|frequen|频繁|too many/i.test(errorText)) return '尝试过于频繁，请稍后再试。'
  if (/not trusted|未信任/i.test(errorText)) return '设备未被信任，请在 Apple 设备上确认信任请求。'
  return ''
}

function mailboxCanClaimAny(mailbox: ICloudMailbox) {
  return Boolean(mailbox.api_active && mailbox.icloud_active && mailbox.status === 'available' && !(mailbox.openai_claimed && mailbox.grok_claimed))
}

function mailboxAvailabilityLabel(mailbox: ICloudMailbox) {
  if (!mailbox.api_active || !mailbox.icloud_active) return '不可用'
  if (mailbox.openai_claimed && mailbox.grok_claimed) return '两平台均已使用'
  if (mailbox.openai_claimed) return '仅 Grok 可用'
  if (mailbox.grok_claimed) return '仅 GPT 可用'
  if (mailbox.status && mailbox.status !== 'available') return '已使用'
  return 'GPT / Grok 均可用'
}

function mailboxAvailabilityTone(mailbox: ICloudMailbox): 'success' | 'info' | 'warning' | 'danger' | 'muted' {
  if (!mailbox.api_active || !mailbox.icloud_active) return 'danger'
  if ((mailbox.openai_claimed && mailbox.grok_claimed) || (mailbox.status && mailbox.status !== 'available')) return 'warning'
  if (mailbox.openai_claimed) return 'info'
  if (mailbox.grok_claimed) return 'success'
  return 'muted'
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : String(error || '请求失败')
}

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function loginStateTone(checked?: boolean, ok?: boolean): 'success' | 'danger' | 'muted' {
  if (!checked) return 'muted'
  return ok === false ? 'danger' : 'success'
}

function loginStateLabel(checked?: boolean, ok?: boolean) {
  if (!checked) return '已保存'
  return ok === false ? '检测异常' : '检测正常'
}

function needsOldICloudLogin(result: ICloudMailboxSyncResult) {
  return /旧接口|icloud\s*web|old\s*(interface|login)|alias|privacy\s*mail/i.test(String(result.error || ''))
}

async function copyText(value: string, successMessage: string) {
  const text = String(value || '').trim()
  if (!text) return
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      const copied = document.execCommand('copy')
      textarea.remove()
      if (!copied) throw new Error('copy_failed')
    }
    notice.value = successMessage
  } catch {
    pageError.value = '复制失败，请检查浏览器剪贴板权限'
  }
}

function showMailboxApi(mailbox: ICloudMailbox) {
  notice.value = mailbox.api_url || '该邮箱没有可用的 API 地址'
}

function copyMailboxEmail(mailbox: ICloudMailbox) {
  return copyText(mailbox.email || '', '邮箱地址已复制')
}

function copyMailboxApi(mailbox: ICloudMailbox) {
  return copyText(mailbox.api_url || '', '邮箱 API 地址已复制')
}

function copyMailboxPair(mailbox: ICloudMailbox) {
  return copyText(`${mailbox.email || ''}----${mailbox.api_url || ''}`, '邮箱和 API 地址已复制')
}

async function loadScheduler() {
  if (!bridge.value?.reachable) return
  try {
    const result = await icloudApi.schedulerStatus()
    scheduler.value = result.scheduler || null
    if (!selectedAccountIds.value.length && scheduler.value?.account_ids?.length) {
      selectedAccountIds.value = scheduler.value.account_ids.filter(Boolean)
    }
  } catch (error) {
    if (scheduler.value?.running) pageError.value = errorText(error)
  }
}

async function loadData() {
  const [sessionResult, accountsResult, mailboxResult] = await Promise.all([
    icloudApi.session(),
    icloudApi.listAccounts(),
    icloudApi.listMailboxes({
      page: mailboxPage.value,
      page_size: mailboxPageSize.value,
      search: mailboxSearch.value.trim() || undefined,
      account_id: activeMailboxAccountId.value || undefined,
    }),
  ])
  sessions.value = (sessionResult.sessions || []).filter(Boolean)
  if (!sessions.value.length && sessionResult.session) sessions.value = [sessionResult.session]
  const sessionIds = sessions.value.map(item => String(item.account_id || '')).filter(Boolean)
  if (!activeMailboxAccountId.value || !sessionIds.includes(activeMailboxAccountId.value)) {
    const nextAccountId = sessionIds[0] || ''
    if (nextAccountId !== activeMailboxAccountId.value) {
      activeMailboxAccountId.value = nextAccountId
      mailboxPage.value = 1
      return loadData()
    }
  }
  const accountIds = new Set((accountsResult.accounts || []).map(item => String(item.id || '')))
  selectedAccountIds.value = selectedAccountIds.value.filter(id => accountIds.has(id))
  if (imapForm.account_id && !accountIds.has(imapForm.account_id)) imapForm.account_id = ''
  mailboxes.value = mailboxResult.mailboxes || []
  mailboxTotal.value = Number(mailboxResult.pagination?.total_all ?? mailboxResult.pagination?.total ?? mailboxes.value.length)
  mailboxTotalPages.value = Math.max(1, Number(mailboxResult.pagination?.total_pages || 1))
  if (mailboxPage.value > mailboxTotalPages.value) mailboxPage.value = mailboxTotalPages.value
  await loadScheduler()
}

async function searchMailboxes() {
  mailboxPage.value = 1
  await loadData()
}

async function changeMailboxPage(offset: number) {
  const next = Math.min(mailboxTotalPages.value, Math.max(1, mailboxPage.value + offset))
  if (next === mailboxPage.value) return
  mailboxPage.value = next
  await loadData()
}

async function selectMailboxAccount(accountId?: string) {
  const next = String(accountId || '').trim()
  if (!next || next === activeMailboxAccountId.value) return
  activeMailboxAccountId.value = next
  mailboxPage.value = 1
  await loadData()
}

async function handleAccountSelectionChange(accountId?: string) {
  const next = String(accountId || '').trim()
  if (!next) return
  if (selectedAccountIds.value.includes(next)) {
    activeMailboxAccountId.value = next
  } else if (activeMailboxAccountId.value === next) {
    activeMailboxAccountId.value = selectedAccountIds.value[0] || sessions.value[0]?.account_id || ''
  }
  mailboxPage.value = 1
  await loadData()
}

async function refreshAll() {
  loading.value = true
  pageError.value = ''
  syncFailures.value = []
  try {
    bridge.value = await icloudApi.bridgeStatus()
    if (bridge.value.reachable) {
      try {
        await icloudApi.syncExistingClaims()
      } catch (error) {
        pageError.value = `历史账号邮箱标签同步失败：${errorText(error)}`
      }
      await loadData()
    }
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    loading.value = false
  }
}

async function startAppleLogin() {
  appleBusy.value = true
  pageError.value = ''
  try {
    const result = await icloudApi.startLogin(appleForm.channel, {
      apple_id: appleForm.apple_id,
      password: appleForm.password,
      two_factor_method: appleForm.two_factor_method,
    })
    if (result.needs_2fa) {
      applePending.value = result
      appleForm.pending_id = result.pending_id || ''
      notice.value = result.message || '请输入 Apple 2FA 验证码'
    } else {
      appleForm.password = ''
      notice.value = result.message || 'Apple 登录成功'
      await loadData()
    }
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    appleBusy.value = false
  }
}

async function submitApple2fa() {
  appleBusy.value = true
  pageError.value = ''
  try {
    const result = await icloudApi.submit2fa(appleForm.channel, {
      pending_id: appleForm.pending_id,
      code: appleForm.code,
    })
    applePending.value = null
    appleForm.pending_id = ''
    appleForm.code = ''
    appleForm.password = ''
    notice.value = result.message || 'Apple 2FA 登录成功'
    await loadData()
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    appleBusy.value = false
  }
}

async function checkSessions() {
  appleBusy.value = true
  try {
    const result = await icloudApi.checkSession({})
    sessions.value = result.sessions || (result.session ? [result.session] : sessions.value)
    notice.value = result.message || '登录态检测完成'
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    appleBusy.value = false
  }
}

async function saveImap() {
  imapBusy.value = true
  try {
    const result = await icloudApi.saveImapLogin({
      account_id: imapForm.account_id || undefined,
      email: imapForm.email,
      app_password: imapForm.app_password,
    })
    sessions.value = result.sessions || sessions.value
    imapForm.app_password = ''
    notice.value = result.message || '取码登录已保存'
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    imapBusy.value = false
  }
}

async function checkImap() {
  imapBusy.value = true
  try {
    const result = await icloudApi.checkImapLogin({ account_id: imapForm.account_id || undefined })
    sessions.value = result.sessions || sessions.value
    notice.value = result.message || '取码登录检测完成'
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    imapBusy.value = false
  }
}

async function createMailboxes() {
  createBusy.value = true
  try {
    const result = await icloudApi.createMailboxes({ account_ids: selectedAccountIds.value, ...createForm })
    notice.value = result.message || `已创建 ${result.mailboxes?.length || 0} 个隐私邮箱`
    await loadData()
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    createBusy.value = false
  }
}

async function startScheduler() {
  schedulerBusy.value = true
  pageError.value = ''
  try {
    const intervalMinutes = Math.max(60, Number(schedulerForm.interval_minutes) || 60)
    schedulerForm.interval_minutes = intervalMinutes
    const result = await icloudApi.startScheduler({
      account_ids: selectedAccountIds.value,
      label: createForm.label,
      note: createForm.note,
      create_channel: schedulerForm.create_channel,
      interval_minutes: intervalMinutes,
      round_interval_seconds: schedulerForm.round_interval_seconds,
    })
    scheduler.value = result.scheduler || scheduler.value
    notice.value = result.message || `定时创建已启动，每 ${intervalMinutes} 分钟执行一轮`
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    schedulerBusy.value = false
  }
}

async function stopScheduler() {
  schedulerBusy.value = true
  pageError.value = ''
  try {
    const result = await icloudApi.stopScheduler()
    scheduler.value = result.scheduler || scheduler.value
    notice.value = result.message || '定时创建已停止'
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    schedulerBusy.value = false
  }
}

async function clearSchedulerLogs() {
  schedulerBusy.value = true
  try {
    const result = await icloudApi.clearSchedulerLogs()
    scheduler.value = result.scheduler || scheduler.value
    notice.value = result.message || '定时创建日志已清除'
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    schedulerBusy.value = false
  }
}

async function syncAllMailboxes() {
  mailboxBusy.value = true
  try {
    const result = await icloudApi.syncMailboxes({})
    syncFailures.value = (result.results || []).filter(item => String(item.error || '').trim())
    notice.value = result.message || '邮箱同步完成'
    await loadData()
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    mailboxBusy.value = false
  }
}

async function syncMailbox(mailbox: ICloudMailbox) {
  if (!mailbox.id) return
  mailboxBusy.value = true
  try {
    await icloudApi.syncMailbox(mailbox.id)
    // 同步后自动展开消息列表并加载最新邮件
    expandedMailboxId.value = mailbox.id
    messageBusyId.value = mailbox.id
    try {
      const result = await icloudApi.listMessages(mailbox.id)
      messagesByMailbox[mailbox.id] = result.messages || []
    } catch {
      // 消息加载失败不影响同步结果提示
    } finally {
      messageBusyId.value = ''
    }
    notice.value = `${mailbox.email || '邮箱'} 已同步`
    // 从失败列表中移除该邮箱
    syncFailures.value = syncFailures.value.filter(f => f.account_id !== mailbox.account_id)
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    mailboxBusy.value = false
  }
}

async function retrySyncFailed() {
  if (syncFailures.value.length === 0) return
  mailboxBusy.value = true
  try {
    const result = await icloudApi.syncMailboxes({})
    syncFailures.value = (result.results || []).filter(item => String(item.error || '').trim())
    notice.value = `重试完成，剩余 ${syncFailures.value.length} 个失败账号`
    await loadData()
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    mailboxBusy.value = false
  }
}

async function fetchCode(mailbox: ICloudMailbox) {
  if (!mailbox.id) return
  codeBusyId.value = mailbox.id
  try {
    const result = await icloudApi.fetchCode(mailbox.id)
    if (result.code) codeByMailbox[mailbox.id] = result.code
    notice.value = result.code ? `已取得 ${mailbox.email || '邮箱'} 的验证码` : result.subject || '暂未收到新验证码'
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    codeBusyId.value = ''
  }
}

async function toggleMessages(mailbox: ICloudMailbox, force = false) {
  if (!mailbox.id) return
  if (!force && expandedMailboxId.value === mailbox.id) {
    expandedMailboxId.value = ''
    return
  }
  messageBusyId.value = mailbox.id
  try {
    const result = await icloudApi.listMessages(mailbox.id)
    messagesByMailbox[mailbox.id] = result.messages || []
    expandedMailboxId.value = mailbox.id
  } catch (error) {
    pageError.value = errorText(error)
  } finally {
    messageBusyId.value = ''
  }
}

onMounted(async () => {
  await refreshAll()
  schedulerTimer = window.setInterval(loadScheduler, 10_000)
})

onBeforeUnmount(() => {
  if (schedulerTimer) window.clearInterval(schedulerTimer)
})
</script>

<style scoped>
.ui-select {
  display: block;
  width: 100%;
  min-height: 2.5rem;
  border: 1px solid hsl(var(--border));
  border-radius: .5rem;
  background: hsl(var(--background));
  padding: .5rem .7rem;
  color: hsl(var(--foreground));
  font-size: .75rem;
}
</style>
