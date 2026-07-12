<template>
  <BaseDialog
    :show="show"
    :title="t('admin.subscriptions.upgradeSubscription')"
    width="normal"
    :close-on-escape="!submitting"
    :show-close-button="!submitting"
    @close="close"
  >
    <div v-if="subscription" class="space-y-5">
      <div class="grid gap-3 text-sm sm:grid-cols-2">
        <div>
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.subscriptions.upgradeSource') }}</div>
          <div class="mt-1 font-medium text-gray-900 dark:text-white">{{ subscription.group?.name || `#${subscription.group_id}` }}</div>
        </div>
        <div>
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.subscriptions.currentExpiration') }}</div>
          <div class="mt-1 font-medium text-gray-900 dark:text-white">{{ formatDateOnly(subscription.expires_at) }}</div>
        </div>
      </div>

      <div>
        <div class="mb-2 text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.subscriptions.upgradeUsage') }}</div>
        <div class="grid grid-cols-2 gap-x-5 gap-y-2 text-sm">
          <div v-for="item in usageItems" :key="item.label" class="flex items-center justify-between border-b border-gray-100 py-1.5 dark:border-dark-700">
            <span class="text-gray-500 dark:text-gray-400">{{ item.label }}</span>
            <span class="font-medium text-gray-900 dark:text-white">${{ item.value.toFixed(2) }}</span>
          </div>
        </div>
      </div>

      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.subscriptions.upgradeTarget') }}
        </label>
        <Select
          v-model="targetGroupId"
          :options="targetOptions"
          :placeholder="t('admin.subscriptions.selectUpgradeTarget')"
          :disabled="loading || submitting"
          :empty-text="t('admin.subscriptions.noUpgradeTargets')"
          searchable
        />
        <p v-if="loading" class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
        <p v-else-if="targetOptions.length === 0" class="mt-2 text-xs text-amber-600 dark:text-amber-400">
          {{ t('admin.subscriptions.noUpgradeTargets') }}
        </p>
      </div>

      <div class="border-l-2 border-amber-400 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('admin.subscriptions.upgradeWarning') }}
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="!canSubmit" @click="submit">
          {{ submitting ? t('admin.subscriptions.upgrading') : t('admin.subscriptions.upgrade') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Group, UserSubscription } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { formatDateOnly } from '@/utils/format'
import subscriptionUpgradeAPI from '@/api/admin/subscriptionUpgrade'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'

const props = defineProps<{
  show: boolean
  subscription: UserSubscription | null
  groups: Group[]
}>()

const emit = defineEmits<{
  close: []
  upgraded: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const submitting = ref(false)
const targetGroupId = ref<number | null>(null)
const existingGroupIDs = ref(new Set<number>())
const idempotencyKey = ref('')

const createIdempotencyKey = (): string => {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

const targetOptions = computed(() => {
  const source = props.subscription
  if (!source?.group) return []
  return props.groups
    .filter(group =>
      group.id !== source.group_id &&
      group.platform === source.group?.platform &&
      group.status === 'active' &&
      group.subscription_type === 'subscription' &&
      !existingGroupIDs.value.has(group.id)
    )
    .map(group => ({ value: group.id, label: group.name }))
})

const usageItems = computed(() => {
  const source = props.subscription
  return [
    { label: t('admin.subscriptions.fiveHour'), value: source?.five_hour_usage_usd ?? 0 },
    { label: t('admin.subscriptions.daily'), value: source?.daily_usage_usd ?? 0 },
    { label: t('admin.subscriptions.weekly'), value: source?.weekly_usage_usd ?? 0 },
    { label: t('admin.subscriptions.monthly'), value: source?.monthly_usage_usd ?? 0 }
  ]
})

const canSubmit = computed(() => !loading.value && !submitting.value && targetGroupId.value !== null)

const loadCandidates = async () => {
  const source = props.subscription
  if (!props.show || !source) return
  loading.value = true
  targetGroupId.value = null
  existingGroupIDs.value = new Set()
  idempotencyKey.value = createIdempotencyKey()
  try {
    const subscriptions = await subscriptionUpgradeAPI.listUserSubscriptions(source.user_id)
    existingGroupIDs.value = new Set(subscriptions.map(item => item.group_id))
    if (targetOptions.value.length === 1) targetGroupId.value = Number(targetOptions.value[0].value)
  } catch (error: unknown) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.subscriptions.errors', t('admin.subscriptions.failedToLoadUpgradeTargets')))
  } finally {
    loading.value = false
  }
}

const close = () => {
  if (submitting.value) return
  emit('close')
}

const submit = async () => {
  const source = props.subscription
  if (!source || targetGroupId.value === null || submitting.value) return
  submitting.value = true
  try {
    await subscriptionUpgradeAPI.upgradeSubscription(
      source.id,
      { target_group_id: targetGroupId.value },
      idempotencyKey.value
    )
    appStore.showSuccess(t('admin.subscriptions.subscriptionUpgraded'))
    emit('upgraded')
  } catch (error: unknown) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.subscriptions.errors', t('admin.subscriptions.failedToUpgrade')))
  } finally {
    submitting.value = false
  }
}

watch(
  () => [props.show, props.subscription?.id] as const,
  ([show]) => {
    if (show) void loadCandidates()
  },
  { immediate: true }
)

watch(targetOptions, options => {
  if (props.show && targetGroupId.value === null && options.length === 1) {
    targetGroupId.value = Number(options[0].value)
  }
})
</script>
