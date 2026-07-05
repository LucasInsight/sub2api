<template>
  <div
    class="flex items-start gap-3 rounded-xl border px-4 py-3 text-left shadow-sm backdrop-blur-sm"
    :class="noticeClass"
  >
    <Icon
      :name="noticeIcon"
      size="md"
      class="mt-0.5 flex-shrink-0"
      :stroke-width="2"
    />
    <div class="min-w-0 space-y-1">
      <p class="text-sm font-semibold">{{ noticeTitle }}</p>
      <p v-if="showMeta" class="break-words text-xs opacity-90">
        {{
          t('home.ipNotice.meta', {
            ip: geo?.ip || '-',
            country: countryLabel
          })
        }}
      </p>
      <p v-if="blockedSummary" class="text-xs font-medium opacity-95">{{ blockedSummary }}</p>
      <p v-if="noticeDescription" class="text-xs leading-relaxed opacity-90">
        {{ noticeDescription }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { CurrentIPGeo } from '@/api/ipGeo'

type NoticeIcon = 'exclamationTriangle' | 'infoCircle'

const props = withDefaults(defineProps<{
  geo?: CurrentIPGeo | null
  pending?: boolean
}>(), {
  geo: null,
  pending: false
})

const { t } = useI18n()

const showMeta = computed(() => !props.pending && !!props.geo)
const countryLabel = computed(() => props.geo?.country_code || t('home.ipNotice.unknownCountry'))

const noticeTitle = computed(() => {
  if (props.pending) {
    return t('home.ipNotice.pendingTitle')
  }
  if (props.geo?.support_status === 'unsupported') {
    return t('home.ipNotice.unsupportedTitle')
  }
  if (props.geo?.support_status === 'supported') {
    return t('home.ipNotice.supportedTitle')
  }
  return t('home.ipNotice.unknownTitle')
})

const noticeDescription = computed(() => {
  if (props.pending) {
    return t('home.ipNotice.pendingDescription')
  }
  if (props.geo?.support_status === 'unsupported') {
    return t('home.ipNotice.unsupportedDescription')
  }
  if (props.geo?.support_status === 'supported') {
    return t('home.ipNotice.supportedDescription')
  }
  return t('home.ipNotice.unknownDescription')
})

const blockedSummary = computed(() => {
  return props.geo?.support_status === 'unsupported' ? t('home.ipNotice.blockedAction') : ''
})

const noticeIcon = computed<NoticeIcon>(() => {
  return props.geo?.support_status === 'unsupported' ? 'exclamationTriangle' : 'infoCircle'
})

const noticeClass = computed(() => {
  if (props.geo?.support_status === 'unsupported') {
    return 'border-amber-300 bg-amber-50/90 text-amber-900 shadow-amber-500/10 dark:border-amber-700/70 dark:bg-amber-950/40 dark:text-amber-100'
  }
  if (props.geo?.support_status === 'supported') {
    return 'border-emerald-300 bg-emerald-50/90 text-emerald-900 shadow-emerald-500/10 dark:border-emerald-700/70 dark:bg-emerald-950/40 dark:text-emerald-100'
  }
  return 'border-gray-200 bg-white/80 text-gray-700 dark:border-dark-700 dark:bg-dark-800/80 dark:text-dark-200'
})
</script>
