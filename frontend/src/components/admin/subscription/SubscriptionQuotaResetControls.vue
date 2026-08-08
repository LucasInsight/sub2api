<template>
  <div class="mt-3 flex w-full flex-wrap items-center justify-end gap-3">
    <button
      type="button"
      data-testid="reset-all-quota-button"
      class="btn"
      :class="resetAllQuotaButtonClass"
      :disabled="!canResetAllQuota"
      :title="resetAllQuotaButtonTitle"
      @click="openResetAllQuotaConfirm"
    >
      <Icon name="refresh" size="md" class="mr-2" />
      {{ t('admin.subscriptions.resetAllQuota') }}
    </button>

    <label
      class="flex min-h-10 items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300"
      :title="t('admin.subscriptions.autoResetQuotaDescription')"
    >
      <span>{{ t('admin.subscriptions.autoResetQuota') }}</span>
      <Toggle
        data-testid="quota-reset-automation-toggle"
        :model-value="Boolean(status?.auto_reset_enabled)"
        :disabled="loadingStatus || updatingAutomation"
        @update:model-value="updateAutomation"
      />
    </label>
  </div>

  <ConfirmDialog
    :show="showConfirm"
    :title="t('admin.subscriptions.resetAllQuotaTitle')"
    :message="t('admin.subscriptions.resetAllQuotaConfirm', { count: status?.active_subscription_count ?? 0 })"
    :confirm-text="t('admin.subscriptions.resetAllQuota')"
    :cancel-text="t('common.cancel')"
    :danger="true"
    :confirm-disabled="!acknowledged || resetting"
    @confirm="confirmReset"
    @cancel="closeConfirm"
  >
    <label class="flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
      <input
        v-model="acknowledged"
        data-testid="reset-all-quota-acknowledgement"
        type="checkbox"
        class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
      />
      <span>{{ t('admin.subscriptions.resetAllQuotaAcknowledgement') }}</span>
    </label>
  </ConfirmDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import {
  getResetAllQuotaStatus,
  resetAllQuota,
  updateQuotaResetAutomation,
  type ResetAllQuotaStatus
} from '@/api/admin/subscriptionQuotaReset'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'

const statusPollIntervalMs = 60_000

const emit = defineEmits<{
  (event: 'reset-completed'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()

const status = ref<ResetAllQuotaStatus | null>(null)
const statusFailed = ref(false)
const loadingStatus = ref(false)
const updatingAutomation = ref(false)
const resetting = ref(false)
const showConfirm = ref(false)
const acknowledged = ref(false)
let statusPollTimer: ReturnType<typeof setInterval> | null = null

const canResetAllQuota = computed(
  () => Boolean(status.value?.enabled) && !loadingStatus.value && !resetting.value
)

const resetAllQuotaButtonClass = computed(() => {
  if (status.value?.automatic_reset_ready) {
    return 'border-amber-500 bg-amber-500 text-white hover:border-amber-600 hover:bg-amber-600 disabled:border-gray-300 disabled:bg-gray-200 disabled:text-gray-400 dark:border-amber-500 dark:bg-amber-500 dark:hover:border-amber-400 dark:hover:bg-amber-400 dark:disabled:border-dark-600 dark:disabled:bg-dark-700 dark:disabled:text-gray-500'
  }
  return 'btn-secondary text-orange-600 disabled:text-gray-400 dark:text-orange-400 dark:disabled:text-gray-500'
})

const resetAllQuotaButtonTitle = computed(() => {
  if (resetting.value) return t('admin.subscriptions.resettingAllQuota')
  if (loadingStatus.value) return t('admin.subscriptions.checkingResetAllQuota')
  if (statusFailed.value) return t('admin.subscriptions.resetAllQuotaStatusFailed')
  if (status.value?.disabled_reason === 'no_active_subscriptions') {
    return t('admin.subscriptions.resetAllQuotaNoActive')
  }
  if (!status.value?.enabled) return t('admin.subscriptions.resetAllQuotaLocked')
  if (status.value.automatic_reset_ready) return t('admin.subscriptions.resetAllQuotaOfficialResetDetected')
  return t('admin.subscriptions.resetAllQuotaReady', {
    count: status.value.active_subscription_count
  })
})

const loadStatus = async () => {
  if (loadingStatus.value) return
  loadingStatus.value = true
  statusFailed.value = false
  try {
    status.value = await getResetAllQuotaStatus()
  } catch (error) {
    status.value = null
    statusFailed.value = true
    console.error('Error loading reset-all quota status:', error)
  } finally {
    loadingStatus.value = false
  }
}

const updateAutomation = async (enabled: boolean) => {
  if (updatingAutomation.value) return
  updatingAutomation.value = true
  try {
    status.value = await updateQuotaResetAutomation(enabled)
    appStore.showSuccess(t('admin.subscriptions.autoResetQuotaUpdated'))
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.subscriptions.failedToUpdateAutoResetQuota'))
    console.error('Error updating quota reset automation:', error)
  } finally {
    updatingAutomation.value = false
  }
}

const createIdempotencyKey = (): string => {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `subscription-reset-all-${crypto.randomUUID()}`
  }
  return `subscription-reset-all-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

const openResetAllQuotaConfirm = () => {
  acknowledged.value = false
  showConfirm.value = true
}

const closeConfirm = () => {
  showConfirm.value = false
  acknowledged.value = false
}

const confirmReset = async () => {
  if (!canResetAllQuota.value || resetting.value || !acknowledged.value) return
  closeConfirm()
  resetting.value = true
  try {
    const result = await resetAllQuota(createIdempotencyKey(), true)
    appStore.showSuccess(t('admin.subscriptions.resetAllQuotaSuccess', { count: result.reset_count }))
    await loadStatus()
    emit('reset-completed')
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.subscriptions.failedToResetAllQuota'))
    console.error('Error resetting all subscription quotas:', error)
    await loadStatus()
  } finally {
    resetting.value = false
  }
}

const handleVisibilityChange = () => {
  if (!document.hidden) void loadStatus()
}

onMounted(() => {
  void loadStatus()
  statusPollTimer = setInterval(() => {
    if (!document.hidden) void loadStatus()
  }, statusPollIntervalMs)
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onUnmounted(() => {
  if (statusPollTimer) clearInterval(statusPollTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>
