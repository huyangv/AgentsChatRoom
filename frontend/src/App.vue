<script setup lang="ts">
import { computed } from 'vue'
import { NConfigProvider, NGlobalStyle, NMessageProvider, darkTheme, dateZhCN, useOsTheme, zhCN } from 'naive-ui'
import ChatRoom from './components/ChatRoom.vue'

// 跟随系统的深色 / 浅色模式
const osTheme = useOsTheme()
const theme = computed(() => (osTheme.value === 'dark' ? darkTheme : null))
</script>

<template>
  <n-config-provider :theme="theme" :locale="zhCN" :date-locale="dateZhCN">
    <n-global-style />
    <n-message-provider>
      <ChatRoom />
    </n-message-provider>
  </n-config-provider>
</template>
