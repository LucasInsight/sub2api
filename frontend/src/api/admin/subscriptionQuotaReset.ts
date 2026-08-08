import { apiClient } from '../client'

export interface ResetAllQuotaStatus {
  enabled: boolean
  auto_reset_enabled: boolean
  pending_event_count: number
  active_subscription_count: number
  eligible_account_count: number
  confirmation_count: number
  required_confirmation_count: number
  automatic_reset_ready: boolean
  latest_detected_at?: string
  last_handled_at?: string
  disabled_reason?: 'no_active_subscriptions'
}

export interface ResetAllQuotaResult {
  reset_count: number
  consumed_event_count: number
  confirmation_count: number
}

export async function getResetAllQuotaStatus(): Promise<ResetAllQuotaStatus> {
  const { data } = await apiClient.get<ResetAllQuotaStatus>(
    '/admin/subscriptions/reset-all-quota/status'
  )
  return data
}

export async function updateQuotaResetAutomation(enabled: boolean): Promise<ResetAllQuotaStatus> {
  const { data } = await apiClient.put<ResetAllQuotaStatus>(
    '/admin/subscriptions/reset-all-quota/automation',
    { enabled }
  )
  return data
}

export async function resetAllQuota(idempotencyKey: string, acknowledged: boolean): Promise<ResetAllQuotaResult> {
  const { data } = await apiClient.post<ResetAllQuotaResult>(
    '/admin/subscriptions/reset-all-quota',
    { acknowledged },
    { headers: { 'Idempotency-Key': idempotencyKey } }
  )
  return data
}
