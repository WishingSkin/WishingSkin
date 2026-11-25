export interface OAuthScopeMeta {
  id: string
  group: 'identity' | 'api'
  label: string
  detail: string
}

export const defaultOAuthClientScopes = ['openid', 'profile', 'email', 'offline_access']
export const defaultNewOAuthClientScopes = ['openid', 'profile']

export const oauthScopeCatalog: OAuthScopeMeta[] = [
  { id: 'openid', group: 'identity', label: '基本身份 (openid)', detail: '确认你的账号唯一标识（用户 ID）。' },
  { id: 'profile', group: 'identity', label: '个人资料 (profile)', detail: '读取你的昵称。' },
  { id: 'email', group: 'identity', label: '邮箱地址 (email)', detail: '读取你的邮箱与验证状态。' },
  { id: 'offline_access', group: 'identity', label: '长期访问 (offline_access)', detail: '在 30 天内自动续期授权，无需重复登录。' },
  { id: 'characters:read', group: 'api', label: '读取角色 (characters:read)', detail: '只读获取你的角色名称、UUID 与穿戴材质。' },
  { id: 'textures:read', group: 'api', label: '读取材质 (textures:read)', detail: '只读列出你上传的皮肤与披风元数据。' },
  { id: 'points:read', group: 'api', label: '读取积分 (points:read)', detail: '只读查询你的积分余额与流水记录。' },
  { id: 'minecraft:read', group: 'api', label: '读取正版绑定 (minecraft:read)', detail: '只读查询你的正版 Minecraft 验证状态与角色名。' },
]

export function oauthScopeLabel(id: string) {
  return oauthScopeCatalog.find(item => item.id === id)?.label || id
}
