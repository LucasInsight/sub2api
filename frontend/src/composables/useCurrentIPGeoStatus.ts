import { computed, ref } from 'vue'
import { getCurrentIPGeo, type CurrentIPGeo } from '@/api/ipGeo'

const currentIPGeo = ref<CurrentIPGeo | null>(null)
const loading = ref(false)
const error = ref<unknown | null>(null)
let pendingRequest: Promise<CurrentIPGeo | null> | null = null

export function useCurrentIPGeoStatus() {
  const isUnsupportedRegion = computed(() => currentIPGeo.value?.support_status === 'unsupported')

  return {
    currentIPGeo,
    loading,
    error,
    isUnsupportedRegion,
    loadCurrentIPGeoStatus
  }
}

export async function loadCurrentIPGeoStatus(options: { force?: boolean } = {}): Promise<CurrentIPGeo | null> {
  if (!options.force && currentIPGeo.value) {
    return currentIPGeo.value
  }
  if (!options.force && pendingRequest) {
    return pendingRequest
  }

  loading.value = true
  pendingRequest = getCurrentIPGeo()
    .then((result) => {
      currentIPGeo.value = result
      error.value = null
      return result
    })
    .catch((err: unknown) => {
      currentIPGeo.value = null
      error.value = err
      console.warn('[IPGeo] Failed to load current IP geo info', err)
      return null
    })
    .finally(() => {
      loading.value = false
      pendingRequest = null
    })

  return pendingRequest
}

export function resetCurrentIPGeoStatusForTest(): void {
  currentIPGeo.value = null
  loading.value = false
  error.value = null
  pendingRequest = null
}
