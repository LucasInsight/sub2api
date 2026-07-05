import { apiClient } from './client'

export type IPGeoSupportStatus = 'supported' | 'unsupported' | 'unknown'

export interface CurrentIPGeo {
  ip: string
  country_code: string
  registered_country_code?: string
  represented_country_code?: string
  country_known: boolean
  is_china: boolean
  supported: boolean
  support_status: IPGeoSupportStatus
}

export async function getCurrentIPGeo(): Promise<CurrentIPGeo> {
  const { data } = await apiClient.get<CurrentIPGeo>('/settings/ip-geo')
  return data
}
