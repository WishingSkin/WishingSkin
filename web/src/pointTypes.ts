export interface PointRules {
  enabled: boolean
  dailyReward: number
  uploadCost: number
  characterCost: number
}

export interface PointCodeItem {
  id: string
  code: string
  points: number
  maxUses: number
  remainingUses: number
  expiresAt: number
  note: string
  disabled: boolean
  createdAt: number
  status: 'available' | 'exhausted' | 'expired' | 'disabled'
}
