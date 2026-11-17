export const themePackGroups = [
  { label: '地上群系', options: [
    { value: 'plains', label: '平原' }, { value: 'snowy', label: '雪原' },
    { value: 'jungle', label: '丛林' }, { value: 'desert', label: '沙漠' },
  ] },
  { label: '地下洞穴', options: [
    { value: 'cave', label: '废弃矿井' }, { value: 'ice_caves', label: '冰洞' },
    { value: 'lush_caves', label: '繁茂洞穴' }, { value: 'desert_temple', label: '沙漠神殿' },
  ] },
  { label: '特别主题', options: [{ value: 'cherry', label: '樱花' }] },
  { label: '下界与末地', options: [
    { value: 'nether_crimson', label: '下界绯红' }, { value: 'nether_warped', label: '下界诡异' },
    { value: 'deep_dark', label: '深暗之域' }, { value: 'end_city', label: '末地城' }, { value: 'void', label: '虚空' },
  ] },
  { label: '辅助主题', options: [
    { value: 'mc_official', label: 'MC 官网配色' }, { value: 'mc_wiki', label: 'Wiki 配色' },
  ] },
] as const

export type ThemePack = 'default' | typeof themePackGroups[number]['options'][number]['value']
export const themePackNames: Record<string, string> = Object.fromEntries([
  ['default', '默认'], ...themePackGroups.flatMap(group => group.options.map(option => [option.value, option.label])),
])
