<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { NButton, NEmpty, NInput, NScrollbar, NTag, NText, useMessage, useThemeVars } from 'naive-ui'
import type { ScrollbarInst } from 'naive-ui'
import { Events } from '@wailsio/runtime'
import { RoomService } from '../../bindings/agentchatroom'
import type { Message, Status } from '../../bindings/agentchatroom/internal/room'

type TagType = 'default' | 'primary' | 'success' | 'info' | 'warning' | 'error'

const agents = ['claude', 'codex']
const senderNames: Record<string, string> = { user: '我', claude: 'Claude', codex: 'Codex', system: '系统' }
const senderTypes: Record<string, TagType> = { user: 'info', claude: 'warning', codex: 'success', system: 'default' }

const themeVars = useThemeVars()
const toast = useMessage()
const messages = ref<Message[]>([])
const status = ref<Status | null>(null)
const draft = ref('')
const sending = ref(false)
const scrollbar = ref<ScrollbarInst | null>(null)

function addMessage(m: Message) {
  if (messages.value.some((x) => x.id === m.id)) return
  messages.value.push(m)
  messages.value.sort((a, b) => a.id - b.id)
  nextTick(() => scrollbar.value?.scrollTo({ top: Number.MAX_SAFE_INTEGER, behavior: 'smooth' }))
}

// 每个 agent 在标题栏显示的状态
function agentState(name: string): { text: string; type: TagType } {
  const s = status.value?.agents?.[name]
  if (s?.waiting) return { text: '等待中', type: 'success' }
  if (s?.pending) return { text: '有新消息待处理', type: 'warning' }
  if (s?.session) return { text: '工作中或已停下', type: 'default' }
  return { text: '未接入', type: 'default' }
}

async function send() {
  const text = draft.value.trim()
  if (!text || sending.value) return
  sending.value = true
  try {
    addMessage(await RoomService.Send(text))
    draft.value = ''
  } catch (err) {
    toast.error('发送失败：' + String(err))
  } finally {
    sending.value = false
  }
}

// Enter 发送，Shift+Enter 换行；输入法选词时不发送
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}

function timeOf(m: Message) {
  return new Date(m.ts).toLocaleTimeString()
}

const unsubscribers: Array<() => void> = []

onMounted(async () => {
  unsubscribers.push(Events.On('room:message', (ev) => addMessage(ev.data)))
  unsubscribers.push(Events.On('room:status', (ev) => { status.value = ev.data }))
  for (const m of (await RoomService.History(0)) ?? []) addMessage(m)
  status.value = await RoomService.Status()
})

onUnmounted(() => unsubscribers.forEach((off) => off()))
</script>

<template>
  <div class="room">
    <header class="bar" :style="{ borderColor: themeVars.dividerColor }">
      <n-text strong class="title">Agent 聊天室</n-text>
      <n-tag v-for="name in agents" :key="name" :type="agentState(name).type" size="small" round>
        {{ senderNames[name] }}：{{ agentState(name).text }}
      </n-tag>
      <n-text depth="3" class="hint">不 @ 任何人时发给全部 agent；@claude / @codex 只发给对方</n-text>
    </header>

    <n-scrollbar ref="scrollbar" class="log">
      <div class="log-inner">
        <n-empty v-if="messages.length === 0" description="还没有消息" class="empty" />
        <div v-for="m in messages" :key="m.id" class="row" :class="{ mine: m.sender === 'user' }">
          <div class="meta">
            <n-tag :type="senderTypes[m.sender] ?? 'default'" size="small" :bordered="false">
              {{ senderNames[m.sender] ?? m.sender }}
            </n-tag>
            <n-text depth="3" class="time">#{{ m.id }} · {{ timeOf(m) }}</n-text>
          </div>
          <div
            class="bubble"
            :style="{
              background: m.sender === 'user' ? themeVars.actionColor : themeVars.cardColor,
              borderColor: themeVars.dividerColor,
            }"
          >{{ m.text }}</div>
        </div>
      </div>
    </n-scrollbar>

    <footer class="composer" :style="{ borderColor: themeVars.dividerColor }">
      <n-input
        v-model:value="draft"
        type="textarea"
        :autosize="{ minRows: 2, maxRows: 6 }"
        placeholder="输入消息，Enter 发送，Shift+Enter 换行"
        @keydown="onKeydown"
      />
      <n-button type="primary" :loading="sending" :disabled="!draft.trim()" @click="send">发送</n-button>
    </footer>
  </div>
</template>

<style scoped>
.room { display: flex; flex-direction: column; height: 100vh; }
.bar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 10px 16px; border-bottom: 1px solid; }
.title { font-size: 16px; margin-right: 6px; }
.hint { font-size: 12px; }
.log { flex: 1; min-height: 0; }
.log-inner { max-width: 880px; margin: 0 auto; padding: 16px; }
.empty { margin-top: 80px; }
.row { display: flex; flex-direction: column; align-items: flex-start; margin-bottom: 14px; }
.row.mine { align-items: flex-end; }
.meta { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.time { font-size: 12px; }
.bubble { max-width: 85%; padding: 8px 12px; border: 1px solid; border-radius: 8px; white-space: pre-wrap; word-break: break-word; line-height: 1.6; }
.composer { display: flex; gap: 8px; align-items: flex-end; padding: 10px 16px; border-top: 1px solid; }
</style>
