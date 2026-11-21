export interface ThemeTextureSlots {
  lightTrim: string
  darkTrim: string
  lightFooter: string
  darkFooter: string
  previewKeys: readonly string[]
}

export interface ThemeSceneSlots {
  light?: string
  dark: string
}

export interface BiomeParticleProfile {
  readonly keys: readonly string[]
  readonly motion: 'fall' | 'rise' | 'float'
  readonly count: number
  readonly sizePx: number
}

export type ThemePack =
  | 'default'
  | 'plains'
  | 'snowy'
  | 'jungle'
  | 'desert'
  | 'cherry'
  | 'nether'
  | 'end'
  | 'deep_dark'
  | 'mc_official'
  | 'mc_wiki'
  | 'nord'
  | 'paper'
  | 'high_contrast'

export interface ThemePackMeta {
  readonly value: ThemePack
  readonly label: string
  readonly lightScene: string
  readonly darkScene: string
  readonly swatches: readonly [string, string, string, string] // lightBg, lightAccent, darkBg, darkAccent
  readonly darkOnly?: boolean
  readonly mcBiome?: boolean
  readonly mcFamily?: boolean
  readonly textures?: ThemeTextureSlots
  readonly scenes?: ThemeSceneSlots
  readonly particles?: {
    readonly light?: BiomeParticleProfile
    readonly dark?: BiomeParticleProfile
  }
}

export interface ThemePackGroup {
  readonly label: string
  readonly options: readonly ThemePackMeta[]
}

export const defaultThemeMeta: ThemePackMeta = {
  value: 'default',
  label: '默认',
  lightScene: '精修浅色（可配 7 种主题色）',
  darkScene: '精修深色',
  swatches: ['#f4f6fa', '#3867d6', '#111111', '#89aaff'],
}

export const themePackGroups: readonly ThemePackGroup[] = [
  {
    label: 'MC 主世界',
    options: [
      {
        value: 'plains',
        label: '平原',
        lightScene: '草原、草方块、橡木',
        darkScene: '废弃矿井、深板岩、矿石',
        swatches: ['#f3f7ef', '#3d6e22', '#14171c', '#56c2d6'],
        mcBiome: true,
        mcFamily: true,
        textures: {
          lightTrim: 'plains_grass_side',
          darkTrim: 'deepslate_diamond_ore',
          lightFooter: 'bedrock',
          darkFooter: 'bedrock',
          previewKeys: ['plains_grass_side', 'plains_grass_top', 'oak_planks', 'deepslate', 'deepslate_diamond_ore', 'rail', 'particle_leaf_plains_0', 'bedrock'],
        },
        scenes: {
          light: '/backgrounds/plains-light.webp',
          dark: '/backgrounds/plains-dark.webp',
        },
        particles: {
          light: { keys: ['particle_leaf_plains_0', 'particle_leaf_plains_1', 'particle_firefly'], motion: 'fall', count: 7, sizePx: 14 },
          dark: { keys: ['particle_spark_0', 'particle_spark_1'], motion: 'float', count: 7, sizePx: 12 },
        },
      },
      {
        value: 'snowy',
        label: '雪原',
        lightScene: '冰封山峰、冰刺、云杉',
        darkScene: '冰洞、发光冰晶、方解石',
        swatches: ['#eef6fb', '#20638c', '#0b1620', '#6ad2f5'],
        mcBiome: true,
        mcFamily: true,
        textures: {
          lightTrim: 'grass_block_side_snowed',
          darkTrim: 'chiseled_packed_ice',
          lightFooter: 'blue_ice',
          darkFooter: 'bedrock',
          previewKeys: ['snow', 'grass_block_side_snowed', 'packed_ice', 'blue_ice', 'ice_crystal', 'icicle_tip', 'packed_ice_bricks', 'calcite'],
        },
        scenes: {
          light: '/backgrounds/snowy-light.webp',
          dark: '/backgrounds/snowy-dark.webp',
        },
        particles: {
          light: { keys: ['particle_spark_0', 'particle_spark_1'], motion: 'fall', count: 9, sizePx: 12 },
          dark: { keys: ['particle_glint', 'particle_spark_0', 'particle_spark_1'], motion: 'float', count: 8, sizePx: 13 },
        },
      },
      {
        value: 'jungle',
        label: '丛林',
        lightScene: '热带雨林、丛林木、竹子',
        darkScene: '繁茂洞穴、苔藓、孢子花',
        swatches: ['#eff7ed', '#256d1b', '#101a11', '#8cc63f'],
        mcBiome: true,
        mcFamily: true,
        textures: {
          lightTrim: 'jungle_grass_side',
          darkTrim: 'moss_block',
          lightFooter: 'bedrock',
          darkFooter: 'bedrock',
          previewKeys: ['jungle_grass_side', 'jungle_leaves', 'jungle_planks', 'bamboo_stalk', 'moss_block', 'clay', 'particle_leaf_jungle_0', 'spore_blossom'],
        },
        scenes: {
          light: '/backgrounds/jungle-light.webp',
          dark: '/backgrounds/jungle-dark.webp',
        },
        particles: {
          light: { keys: ['particle_leaf_jungle_0', 'particle_leaf_jungle_1', 'particle_firefly'], motion: 'fall', count: 8, sizePx: 14 },
          dark: { keys: ['particle_glow', 'particle_firefly'], motion: 'fall', count: 8, sizePx: 12 },
        },
      },
      {
        value: 'desert',
        label: '沙漠',
        lightScene: '沙漠、砂岩、红沙',
        darkScene: '沙漠神殿密室、彩陶考古',
        swatches: ['#faf5ea', '#945218', '#1c1610', '#ec954d'],
        mcBiome: true,
        mcFamily: true,
        textures: {
          lightTrim: 'chiseled_sandstone',
          darkTrim: 'orange_terracotta',
          lightFooter: 'sandstone_top',
          darkFooter: 'bedrock',
          previewKeys: ['sand', 'sandstone', 'chiseled_sandstone', 'red_sand', 'orange_terracotta', 'blue_terracotta', 'suspicious_sand', 'tnt_side'],
        },
        scenes: {
          light: '/backgrounds/desert-light.webp',
          dark: '/backgrounds/desert-dark.webp',
        },
        particles: {
          light: { keys: ['particle_spark_0', 'particle_spark_1'], motion: 'float', count: 6, sizePx: 10 },
          dark: { keys: ['particle_spark_0', 'particle_flame'], motion: 'float', count: 7, sizePx: 12 },
        },
      },
      {
        value: 'cherry',
        label: '樱花',
        lightScene: '樱花树林、像素花瓣飘落',
        darkScene: '紫水晶洞、方解石、晶簇',
        swatches: ['#fdf5f8', '#a8325c', '#17101f', '#c692f7'],
        mcBiome: true,
        mcFamily: true,
        textures: {
          lightTrim: 'cherry_leaves',
          darkTrim: 'amethyst_block',
          lightFooter: 'cherry_log',
          darkFooter: 'bedrock',
          previewKeys: ['cherry_leaves', 'cherry_log', 'cherry_planks', 'particle_cherry_0', 'calcite', 'amethyst_block', 'budding_amethyst', 'amethyst_cluster'],
        },
        scenes: {
          light: '/backgrounds/cherry-light.webp',
          dark: '/backgrounds/cherry-dark.webp',
        },
        particles: {
          light: { keys: ['particle_cherry_0', 'particle_cherry_1', 'particle_cherry_2', 'particle_cherry_3'], motion: 'fall', count: 9, sizePx: 16 },
          dark: { keys: ['particle_glint', 'particle_glow'], motion: 'float', count: 8, sizePx: 14 },
        },
      },
    ],
  },
  {
    label: 'MC 维度',
    options: [
      {
        value: 'nether',
        label: '下界',
        lightScene: '绯红森林、菌光体',
        darkScene: '诡异森林、黑石玄武岩',
        swatches: ['#fbf2f3', '#9e2a3b', '#0b191c', '#3ec4cb'],
        mcBiome: true,
        mcFamily: true,
        textures: {
          lightTrim: 'crimson_nylium_side',
          darkTrim: 'warped_nylium_side',
          lightFooter: 'nether_bricks',
          darkFooter: 'blackstone',
          previewKeys: ['crimson_nylium_side', 'crimson_planks', 'nether_wart_block', 'shroomlight', 'warped_nylium_side', 'warped_planks', 'particle_flame', 'particle_soul_flame'],
        },
        scenes: {
          light: '/backgrounds/nether-light.webp',
          dark: '/backgrounds/nether-dark.webp',
        },
        particles: {
          light: { keys: ['particle_flame', 'particle_spark_0'], motion: 'rise', count: 8, sizePx: 14 },
          dark: { keys: ['particle_soul_flame', 'particle_glow'], motion: 'rise', count: 8, sizePx: 14 },
        },
      },
      {
        value: 'end',
        label: '末地',
        lightScene: '末地城、紫珀与末地石砖',
        darkScene: '虚空外岛、黑曜石',
        swatches: ['#f6f3fa', '#6c4687', '#0d0b14', '#b896f0'],
        mcBiome: true,
        mcFamily: true,
        textures: {
          lightTrim: 'purpur_block',
          darkTrim: 'crying_obsidian',
          lightFooter: 'end_stone',
          darkFooter: 'obsidian',
          previewKeys: ['end_stone', 'end_stone_bricks', 'purpur_block', 'purpur_pillar', 'obsidian', 'crying_obsidian', 'gilded_blackstone', 'particle_glint'],
        },
        scenes: {
          light: '/backgrounds/end-light.webp',
          dark: '/backgrounds/end-dark.webp',
        },
        particles: {
          light: { keys: ['particle_glint', 'particle_spark_0'], motion: 'float', count: 7, sizePx: 14 },
          dark: { keys: ['particle_glint', 'particle_glow'], motion: 'float', count: 8, sizePx: 14 },
        },
      },
      {
        value: 'deep_dark',
        label: '深暗之域',
        lightScene: '仅深色模式',
        darkScene: '远古城市、幽匿传感、强化深板岩',
        swatches: ['#071117', '#2cc5d4', '#071117', '#2cc5d4'],
        darkOnly: true,
        mcBiome: true,
        mcFamily: true,
        textures: {
          lightTrim: 'sculk',
          darkTrim: 'sculk',
          lightFooter: 'reinforced_deepslate_top',
          darkFooter: 'reinforced_deepslate_top',
          previewKeys: ['sculk', 'sculk_vein', 'sculk_catalyst_top', 'sculk_sensor_top', 'particle_sculk_soul_0', 'reinforced_deepslate_top', 'deepslate', 'tuff'],
        },
        scenes: {
          dark: '/backgrounds/deep_dark-dark.webp',
        },
        particles: {
          dark: { keys: ['particle_sculk_soul_0', 'particle_sculk_soul_1', 'particle_sculk_soul_2'], motion: 'rise', count: 7, sizePx: 18 },
        },
      },
    ],
  },
  {
    label: 'MC 社区风',
    options: [
      {
        value: 'mc_official',
        label: 'MC 官网风',
        lightScene: '深炭顶栏、草绿按压按钮',
        darkScene: '官网深炭风格',
        swatches: ['#f2f2f2', '#3c8527', '#171615', '#6bc349'],
        mcFamily: true,
      },
      {
        value: 'mc_wiki',
        label: 'MC Wiki 风',
        lightScene: '石质顶栏、经典蓝链与工具书框线',
        darkScene: 'Wiki 深灰夜间阅读',
        swatches: ['#e8f0f4', '#295bb5', '#1d2022', '#69b1ff'],
        mcFamily: true,
      },
    ],
  },
  {
    label: '通用主题',
    options: [
      {
        value: 'nord',
        label: 'Nord 极光',
        lightScene: '北欧雪霜冷灰蓝',
        darkScene: '极夜冷蓝深灰',
        swatches: ['#eceff4', '#4c6d99', '#2e3440', '#88c0d0'],
      },
      {
        value: 'paper',
        label: '纸张阅读',
        lightScene: '暖白宣纸底色，适合长阅读',
        darkScene: '暖墨深棕低眩光',
        swatches: ['#f8f4eb', '#8c4f24', '#1b1814', '#dba16a'],
      },
      {
        value: 'high_contrast',
        label: '高对比无障碍',
        lightScene: '纯白黑字、加粗边框与清晰焦点',
        darkScene: '纯黑白字、高对比蓝亮焦点',
        swatches: ['#ffffff', '#0038a8', '#000000', '#6ec1ff'],
      },
    ],
  },
]

export const allThemePacks: readonly ThemePackMeta[] = [
  defaultThemeMeta,
  ...themePackGroups.flatMap(group => group.options),
]

export const themePackById: Record<string, ThemePackMeta> = Object.fromEntries(
  allThemePacks.map(meta => [meta.value, meta]),
)

export const themePackNames: Record<string, string> = Object.fromEntries(
  allThemePacks.map(meta => [meta.value, meta.label]),
)

const legacyThemePacks: Readonly<Partial<Record<string, ThemePack>>> = {
  cave: 'plains',
  ice_caves: 'snowy',
  lush_caves: 'jungle',
  desert_temple: 'desert',
  nether_crimson: 'nether',
  nether_warped: 'nether',
  end_city: 'end',
  void: 'end',
}

export function resolveUserThemePack(value: string | null): ThemePack | '' {
  if (!value) return ''
  if (Object.hasOwn(themePackById, value)) return themePackById[value].value
  return Object.hasOwn(legacyThemePacks, value) ? legacyThemePacks[value] ?? '' : ''
}

export function isDarkOnlyTheme(pack?: string): boolean {
  return Boolean(pack && themePackById[pack]?.darkOnly)
}

export function isMCBiomeTheme(pack?: string): boolean {
  return Boolean(pack && themePackById[pack]?.mcBiome)
}

export function isMCFamilyTheme(pack?: string): boolean {
  return Boolean(pack && themePackById[pack]?.mcFamily)
}

export const fontPresets = [
  { value: 'system', label: '系统默认', description: '清晰的原生无衬线字体，零额外下载' },
  { value: 'monocraft', label: 'Monocraft（仅英文与数字）', description: '英文与数字使用经典 MC 像素体，中文保持系统清晰字体' },
  { value: 'fusion_pixel', label: '缝合像素（Fusion Pixel 12px）', description: '英文 Monocraft + 简体中文 12px 精致像素点阵' },
  { value: 'ark_pixel', label: '方舟像素（Ark Pixel 12px）', description: '英文 Monocraft + 简体中文方正硬朗 12px 像素体' },
  { value: 'custom', label: '自定义字体', description: '使用站长上传的 WOFF2 字体或外部字体地址' },
] as const

export type FontPreset = typeof fontPresets[number]['value']

export const fontScopes = [
  { value: 'headings', label: '仅标题与品牌' },
  { value: 'all', label: '全局界面' },
] as const

export type FontScope = typeof fontScopes[number]['value']

export const fontPresetNames: Record<string, string> = Object.fromEntries(
  fontPresets.map(item => [item.value, item.label]),
)

export const fontScopeNames: Record<string, string> = Object.fromEntries(
  fontScopes.map(item => [item.value, item.label]),
)
