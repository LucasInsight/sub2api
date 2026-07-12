import { apiClient } from '../client'
import type { UserSubscription } from '@/types'

export interface UpgradeSubscriptionRequest {
  target_group_id: number
}

export interface UpgradeSubscriptionResponse {
  source_subscription_id: number
  subscription: UserSubscription
  migrated_api_key_count: number
}

export async function listUserSubscriptions(userId: number): Promise<UserSubscription[]> {
  const { data } = await apiClient.get<UserSubscription[]>(`/admin/users/${userId}/subscriptions`)
  return data
}

export async function upgradeSubscription(
  subscriptionId: number,
  request: UpgradeSubscriptionRequest,
  idempotencyKey: string
): Promise<UpgradeSubscriptionResponse> {
  const { data } = await apiClient.post<UpgradeSubscriptionResponse>(
    `/admin/subscriptions/${subscriptionId}/upgrade`,
    request,
    { headers: { 'Idempotency-Key': idempotencyKey } }
  )
  return data
}

export const subscriptionUpgradeAPI = {
  listUserSubscriptions,
  upgradeSubscription
}

export default subscriptionUpgradeAPI
