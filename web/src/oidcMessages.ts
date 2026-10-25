export const oidcMessages: Record<string, string> = {
  expired: 'OIDC 登录已失效，请重试。',
  cancelled: 'OIDC 登录已取消。',
  failed: 'OIDC 登录未完成，请重试或联系管理员。',
  registration: '此 OIDC 身份尚未绑定，注册当前已关闭。请先用本地账号登录并绑定。',
  linked: '此 OIDC 身份或当前账号已绑定，请检查后重试。',
  account: '账号不可用或尚未验证邮箱，请使用本地账号检查。',
  session: '账号会话已变化，请重新开始绑定或登录。',
}
