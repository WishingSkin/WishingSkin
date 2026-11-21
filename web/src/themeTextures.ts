import type { PublicThemeAssets } from './accountTypes.ts'
import { themePackById } from './themePacks.ts'

interface ProceduralTileSpec {
  base: string
  specks: readonly [string, number][] // color, modulo threshold
  bandTop?: { height: number; color: string; dripColor?: string }
  grid?: { color: string; stepX: number; stepY: number }
  accentPixels?: readonly [number, number, string][]
}

const tileSpecs: Record<string, ProceduralTileSpec> = {
  plains_grass_side: { base: '#866043', specks: [['#6d4c33', 3], ['#9b7251', 5]], bandTop: { height: 4, color: '#6ea83d', dripColor: '#568c2c' } },
  plains_grass_top: { base: '#79b348', specks: [['#669c38', 3], ['#8cc757', 4], ['#5b8f30', 7]] },
  dirt: { base: '#866043', specks: [['#6f4d34', 3], ['#9c7454', 5], ['#5d3e28', 9]] },
  stone: { base: '#7d7d7d', specks: [['#6e6e6e', 3], ['#8e8e8e', 4], ['#626262', 7]] },
  oak_planks: { base: '#a68553', specks: [['#967545', 4], ['#b69460', 6]], grid: { color: '#7c5f35', stepX: 16, stepY: 4 } },
  deepslate: { base: '#494950', specks: [['#3b3b42', 3], ['#5a5a63', 4], ['#303036', 7]] },
  deepslate_diamond_ore: {
    base: '#494950',
    specks: [['#3a3a41', 3], ['#585861', 5]],
    accentPixels: [[3, 4, '#56d8e6'], [4, 4, '#8ff5ff'], [4, 5, '#34b4c4'], [11, 3, '#56d8e6'], [12, 4, '#8ff5ff'], [8, 10, '#56d8e6'], [9, 10, '#8ff5ff'], [9, 11, '#34b4c4'], [2, 12, '#4bc9d9']],
  },
  rail: { base: '#4d4d54', specks: [['#3f3f46', 4]], grid: { color: '#9c7848', stepX: 16, stepY: 4 }, accentPixels: [[3, 0, '#b8bec4'], [3, 4, '#b8bec4'], [3, 8, '#b8bec4'], [3, 12, '#b8bec4'], [12, 0, '#b8bec4'], [12, 4, '#b8bec4'], [12, 8, '#b8bec4'], [12, 12, '#b8bec4']] },
  coal_ore: { base: '#4d4d54', specks: [['#3d3d44', 4]], accentPixels: [[3, 4, '#1c1c20'], [4, 5, '#25252b'], [10, 6, '#1c1c20'], [11, 7, '#161619'], [6, 11, '#1c1c20'], [7, 12, '#25252b']] },
  bedrock: { base: '#3d3d3d', specks: [['#1f1f1f', 2], ['#616161', 3], ['#2b2b2b', 5], ['#797979', 8]] },

  snow: { base: '#eef7fb', specks: [['#dcecf5', 3], ['#ffffff', 4]] },
  grass_block_side_snowed: { base: '#866043', specks: [['#6d4c33', 4], ['#9b7251', 6]], bandTop: { height: 5, color: '#eef7fb', dripColor: '#d0e6f2' } },
  packed_ice: { base: '#8cb4e0', specks: [['#7aa4d4', 3], ['#a6caf0', 4], ['#c4e2fa', 8]] },
  blue_ice: { base: '#6a9ee6', specks: [['#588cd6', 3], ['#85b7f5', 5], ['#a9d2ff', 9]] },
  packed_ice_bricks: { base: '#7aa9dc', specks: [['#6b98cc', 4], ['#96c1ee', 5]], grid: { color: '#537eb2', stepX: 8, stepY: 8 } },
  ice_crystal: {
    base: '#193a54',
    specks: [['#234d6d', 3], ['#132d42', 5]],
    accentPixels: [[7, 2, '#9cecff'], [7, 3, '#6ad2f5'], [8, 4, '#c8f8ff'], [6, 5, '#6ad2f5'], [7, 6, '#ffffff'], [8, 7, '#6ad2f5'], [7, 8, '#46b5df'], [3, 10, '#85e4ff'], [12, 11, '#85e4ff']],
  },
  icicle_tip: { base: '#6aa4cc', specks: [['#8ec6e8', 3], ['#b8e4fa', 6]] },
  calcite: { base: '#e2e5e0', specks: [['#cfd4cd', 3], ['#eff2ed', 4], ['#bfc6be', 8]] },
  polished_calcite_bricks: { base: '#dde1dc', specks: [['#cdd2cc', 4], ['#edf0eb', 5]], grid: { color: '#b4bbb3', stepX: 8, stepY: 8 } },

  jungle_grass_side: { base: '#7e593d', specks: [['#66452e', 3], ['#946a49', 5]], bandTop: { height: 4, color: '#46a82d', dripColor: '#32871d' } },
  jungle_leaves: { base: '#3d8f24', specks: [['#2e7319', 2], ['#54ab36', 4], ['#235a12', 7]] },
  jungle_planks: { base: '#a37552', specks: [['#8f6343', 4], ['#b68662', 6]], grid: { color: '#784f33', stepX: 16, stepY: 4 } },
  bamboo_stalk: { base: '#639b28', specks: [['#4f801d', 3], ['#7db73a', 5]], grid: { color: '#3f6916', stepX: 8, stepY: 4 } },
  rooted_dirt: { base: '#7f5b3f', specks: [['#68482f', 3], ['#a17a54', 5], ['#91704d', 7]] },
  mossy_cobblestone: { base: '#6f7868', specks: [['#586353', 3], ['#4f7d2f', 4], ['#868c82', 6]], grid: { color: '#4b5247', stepX: 8, stepY: 8 } },
  moss_block: { base: '#5e7e28', specks: [['#4c691e', 2], ['#769a36', 4], ['#8db147', 8]] },
  clay: { base: '#9da5b2', specks: [['#8c95a3', 3], ['#aeb6c2', 5]] },
  glow_lichen: { base: '#415548', specks: [['#587360', 4]], accentPixels: [[3, 3, '#a4e8cf'], [4, 4, '#c7faea'], [10, 8, '#a4e8cf'], [11, 9, '#c7faea'], [6, 13, '#88d6ba']] },
  spore_blossom: { base: '#4a6b24', specks: [['#5d8230', 4]], accentPixels: [[6, 6, '#e288b4'], [7, 6, '#f4a8cc'], [8, 6, '#e288b4'], [7, 7, '#f5db76'], [7, 8, '#e288b4']] },

  sand: { base: '#dfd0a4', specks: [['#d1be8c', 3], ['#ece0b9', 4], ['#c4b07c', 8]] },
  sandstone: { base: '#d9c693', specks: [['#cbb57d', 3], ['#e6d6a9', 5]], bandTop: { height: 4, color: '#e3d3a5' } },
  sandstone_top: { base: '#e2d2a3', specks: [['#d3c08d', 3], ['#efe3be', 5]] },
  cut_sandstone: { base: '#dbca9a', specks: [['#cdb882', 4]], grid: { color: '#bea771', stepX: 16, stepY: 8 } },
  chiseled_sandstone: { base: '#d7c38e', specks: [['#c9b27a', 4]], grid: { color: '#b09760', stepX: 8, stepY: 8 }, accentPixels: [[6, 6, '#a68b52'], [9, 6, '#a68b52'], [7, 9, '#a68b52'], [8, 9, '#a68b52']] },
  red_sand: { base: '#be6627', specks: [['#a9551b', 3], ['#cf7838', 5]] },
  orange_terracotta: { base: '#a15325', specks: [['#8e461d', 3], ['#b36232', 5]], grid: { color: '#7e3b17', stepX: 8, stepY: 8 } },
  blue_terracotta: { base: '#4a3d5d', specks: [['#3e3250', 3], ['#5a4c6e', 5]] },
  suspicious_sand: { base: '#d8c593', specks: [['#c5ae76', 3], ['#e5d5a7', 5]], accentPixels: [[5, 5, '#8c693b'], [6, 5, '#a8834d'], [7, 6, '#8c693b'], [9, 10, '#8c693b'], [10, 10, '#a8834d']] },
  tnt_side: { base: '#c43e2b', specks: [['#ad3221', 4]], bandTop: { height: 5, color: '#ebe4d8' } },

  cherry_grass_side: { base: '#866043', specks: [['#6e4d34', 3], ['#9b7251', 5]], bandTop: { height: 4, color: '#a5cc58', dripColor: '#8eb543' } },
  cherry_leaves: { base: '#f3b7ca', specks: [['#e59db5', 2], ['#fbd0df', 4], ['#d785a1', 7]] },
  cherry_log: { base: '#3d242e', specks: [['#2f1a23', 3], ['#533240', 5]], grid: { color: '#27141c', stepX: 4, stepY: 16 } },
  cherry_planks: { base: '#e6b8c4', specks: [['#daa6b4', 4], ['#f0c8d2', 6]], grid: { color: '#c890a0', stepX: 16, stepY: 4 } },
  pink_petals: { base: '#a6cc58', specks: [['#94b948', 4]], accentPixels: [[3, 3, '#f6b8cc'], [4, 4, '#ffd9e6'], [10, 6, '#f6b8cc'], [11, 7, '#ffd9e6'], [6, 12, '#f6b8cc']] },
  amethyst_block: { base: '#875ec4', specks: [['#724aad', 3], ['#a178dd', 4], ['#c8a6fa', 8]] },
  budding_amethyst: { base: '#7c53b8', specks: [['#6740a0', 3], ['#9970d6', 5]], accentPixels: [[4, 4, '#d9baff'], [11, 5, '#d9baff'], [6, 11, '#d9baff'], [10, 10, '#c293fa']] },
  amethyst_cluster: { base: '#321c47', specks: [['#261438', 4]], accentPixels: [[7, 3, '#e3c7ff'], [7, 4, '#c08df7'], [8, 5, '#9d67dc'], [6, 6, '#c08df7'], [9, 8, '#e3c7ff'], [5, 9, '#b17ceb']] },

  crimson_nylium: { base: '#852532', specks: [['#6f1d28', 2], ['#9c3341', 4], ['#b34452', 8]] },
  crimson_nylium_side: { base: '#6b2a2a', specks: [['#572020', 3], ['#7d3535', 5]], bandTop: { height: 5, color: '#9c2635', dripColor: '#7a1c28' } },
  crimson_planks: { base: '#6f324b', specks: [['#5e273e', 4], ['#813e59', 6]], grid: { color: '#4d1e32', stepX: 16, stepY: 4 } },
  nether_wart_block: { base: '#78131d', specks: [['#600d15', 2], ['#931d29', 4], ['#ad2b38', 8]] },
  netherrack: { base: '#6a2c2b', specks: [['#562120', 2], ['#7d3938', 4], ['#471817', 7]] },
  nether_bricks: { base: '#341a20', specks: [['#2a1419', 4]], grid: { color: '#1d0d11', stepX: 8, stepY: 4 } },
  shroomlight: { base: '#ee9a45', specks: [['#df8130', 3], ['#fac06e', 4], ['#fff0a6', 8]] },
  warped_nylium: { base: '#1e7874', specks: [['#16615e', 2], ['#2c9690', 4], ['#41b5ad', 8]] },
  warped_nylium_side: { base: '#5f2b2b', specks: [['#4d2222', 3], ['#713636', 5]], bandTop: { height: 5, color: '#228783', dripColor: '#176b68' } },
  warped_planks: { base: '#286c6a', specks: [['#205b59', 4], ['#337f7c', 6]], grid: { color: '#194948', stepX: 16, stepY: 4 } },
  basalt_side: { base: '#4a4a52', specks: [['#3b3b42', 3], ['#5d5d66', 5]], grid: { color: '#33333a', stepX: 4, stepY: 16 } },
  blackstone: { base: '#2c2730', specks: [['#211d24', 2], ['#3c3642', 4], ['#18151b', 7]] },

  end_stone: { base: '#dddba0', specks: [['#cbc88a', 3], ['#ebe9b8', 4], ['#b9b678', 8]] },
  end_stone_bricks: { base: '#d9d89c', specks: [['#c9c78b', 4], ['#e7e6b2', 6]], grid: { color: '#aeac72', stepX: 8, stepY: 8 } },
  purpur_block: { base: '#aa7eaa', specks: [['#966b96', 3], ['#bd93bd', 5]], grid: { color: '#7d567d', stepX: 8, stepY: 8 } },
  purpur_pillar: { base: '#ac82ac', specks: [['#9a709a', 4]], grid: { color: '#7d567d', stepX: 4, stepY: 16 } },
  obsidian: { base: '#171024', specks: [['#241938', 3], ['#0f0a18', 4], ['#3a2759', 8]] },
  crying_obsidian: {
    base: '#181126',
    specks: [['#251a3b', 3], ['#0f0a18', 5]],
    accentPixels: [[4, 3, '#8f3cf7'], [4, 4, '#c18aff'], [11, 6, '#8f3cf7'], [11, 7, '#c18aff'], [6, 11, '#8f3cf7'], [7, 12, '#a75eff']],
  },
  gilded_blackstone: {
    base: '#2c2730',
    specks: [['#201c24', 3], ['#3b3540', 5]],
    accentPixels: [[3, 4, '#e5b83b'], [4, 4, '#fae078'], [10, 7, '#e5b83b'], [11, 8, '#c79624'], [6, 12, '#fae078']],
  },

  sculk: {
    base: '#081b21',
    specks: [['#0c262e', 3], ['#051217', 4]],
    accentPixels: [[2, 3, '#178a94'], [3, 3, '#33cbd4'], [10, 4, '#178a94'], [11, 5, '#33cbd4'], [6, 9, '#23aeb8'], [7, 10, '#5ae5ec'], [13, 12, '#178a94'], [3, 13, '#23aeb8']],
  },
  sculk_vein: { base: '#091f26', specks: [['#0f3038', 4]], accentPixels: [[2, 2, '#2ac2cc'], [5, 6, '#4de3ec'], [11, 9, '#2ac2cc'], [8, 13, '#4de3ec']] },
  sculk_catalyst_top: { base: '#d9d5c0', specks: [['#c4bfaa', 3], ['#0d2830', 5]], accentPixels: [[6, 6, '#2ac2cc'], [9, 9, '#2ac2cc'], [7, 8, '#53e8f0']] },
  sculk_sensor_top: { base: '#0c2d36', specks: [['#123c47', 3]], accentPixels: [[4, 4, '#36d2dc'], [11, 4, '#36d2dc'], [4, 11, '#36d2dc'], [11, 11, '#36d2dc']] },
  sculk_shrieker_top: { base: '#d2ceb9', specks: [['#0b2229', 3]], grid: { color: '#0a2026', stepX: 4, stepY: 4 } },
  reinforced_deepslate_top: { base: '#3d4447', specks: [['#2f3538', 3], ['#535d61', 5]], grid: { color: '#1f2528', stepX: 16, stepY: 16 } },
  reinforced_deepslate_side: { base: '#383e42', specks: [['#2b3033', 3], ['#4d555a', 5]], bandTop: { height: 3, color: '#697270' } },
  tuff: { base: '#5f625c', specks: [['#4f524c', 3], ['#70736d', 4]] },
}

const proceduralCache = new Map<string, string>()

function hashCoord(seed: number, x: number, y: number): number {
  let n = (seed * 73856093) ^ (x * 19349663) ^ (y * 83492791)
  n = ((n >> 13) ^ n) * 1274126177
  return ((n >> 16) ^ n) >>> 0
}

function keySeed(key: string): number {
  let h = 2166136261
  for (let i = 0; i < key.length; i++) {
    h ^= key.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

const pixelParticleSprites: Record<string, { size: number; pixels: readonly [number, number, string][] }> = {
  particle_cherry_0: {
    size: 8,
    pixels: [[2, 2, '#f49cb8'], [3, 2, '#ffd6e5'], [4, 2, '#f49cb8'], [1, 3, '#e882a3'], [2, 3, '#ffd6e5'], [3, 3, '#ffb8d0'], [4, 3, '#f49cb8'], [2, 4, '#e882a3'], [3, 4, '#f49cb8'], [4, 4, '#e882a3'], [3, 5, '#d66d91']],
  },
  particle_cherry_1: {
    size: 8,
    pixels: [[3, 1, '#ffd6e5'], [2, 2, '#f49cb8'], [3, 2, '#ffb8d0'], [4, 2, '#ffd6e5'], [2, 3, '#e882a3'], [3, 3, '#f49cb8'], [4, 3, '#e882a3'], [3, 4, '#d66d91']],
  },
  particle_cherry_2: {
    size: 8,
    pixels: [[2, 2, '#ffb8d0'], [3, 2, '#ffd6e5'], [1, 3, '#f49cb8'], [2, 3, '#ffd6e5'], [3, 3, '#f49cb8'], [4, 3, '#e882a3'], [2, 4, '#e882a3'], [3, 4, '#d66d91']],
  },
  particle_cherry_3: {
    size: 8,
    pixels: [[3, 2, '#ffd6e5'], [4, 2, '#f49cb8'], [2, 3, '#f49cb8'], [3, 3, '#ffb8d0'], [4, 3, '#e882a3'], [3, 4, '#e882a3']],
  },
  particle_leaf_plains_0: {
    size: 8,
    pixels: [[3, 1, '#8cc757'], [2, 2, '#6ea83d'], [3, 2, '#8cc757'], [4, 2, '#6ea83d'], [2, 3, '#568c2c'], [3, 3, '#6ea83d'], [4, 3, '#568c2c'], [3, 4, '#477822']],
  },
  particle_leaf_plains_1: {
    size: 8,
    pixels: [[2, 2, '#8cc757'], [3, 2, '#6ea83d'], [1, 3, '#6ea83d'], [2, 3, '#568c2c'], [3, 3, '#6ea83d'], [4, 3, '#477822'], [2, 4, '#477822']],
  },
  particle_leaf_jungle_0: {
    size: 8,
    pixels: [[3, 1, '#59c93c'], [2, 2, '#3d9e24'], [3, 2, '#59c93c'], [4, 2, '#3d9e24'], [2, 3, '#2b7d17'], [3, 3, '#3d9e24'], [4, 3, '#2b7d17'], [3, 4, '#1f610f']],
  },
  particle_leaf_jungle_1: {
    size: 8,
    pixels: [[2, 2, '#59c93c'], [3, 2, '#3d9e24'], [1, 3, '#3d9e24'], [2, 3, '#59c93c'], [3, 3, '#2b7d17'], [2, 4, '#1f610f']],
  },
  particle_firefly: {
    size: 8,
    pixels: [[3, 3, '#fff9b0'], [4, 3, '#ffe45e'], [3, 4, '#ffe45e'], [4, 4, '#d9a520']],
  },
  particle_sculk_soul_0: {
    size: 8,
    pixels: [[3, 1, '#7cf7ff'], [4, 1, '#7cf7ff'], [2, 2, '#3ad9e6'], [3, 2, '#c8feff'], [4, 2, '#c8feff'], [5, 2, '#3ad9e6'], [2, 3, '#1ea8b5'], [3, 3, '#0b2229'], [4, 3, '#3ad9e6'], [5, 3, '#1ea8b5'], [2, 4, '#1ea8b5'], [3, 4, '#3ad9e6'], [4, 4, '#0b2229'], [5, 4, '#1ea8b5'], [3, 5, '#178a94'], [4, 5, '#178a94'], [3, 6, '#0f5d66']],
  },
  particle_sculk_soul_1: {
    size: 8,
    pixels: [[3, 2, '#7cf7ff'], [4, 2, '#7cf7ff'], [2, 3, '#3ad9e6'], [3, 3, '#c8feff'], [4, 3, '#c8feff'], [5, 3, '#3ad9e6'], [3, 4, '#1ea8b5'], [4, 4, '#3ad9e6'], [3, 5, '#178a94']],
  },
  particle_sculk_soul_2: {
    size: 8,
    pixels: [[3, 2, '#5ae5ec'], [4, 2, '#5ae5ec'], [3, 3, '#c8feff'], [4, 3, '#7cf7ff'], [3, 4, '#1ea8b5']],
  },
  particle_flame: {
    size: 8,
    pixels: [[3, 1, '#fff3a8'], [3, 2, '#ffbe42'], [4, 2, '#ff9824'], [2, 3, '#ff9824'], [3, 3, '#fff3a8'], [4, 3, '#ff6d1f'], [2, 4, '#e04819'], [3, 4, '#ff9824'], [4, 4, '#e04819'], [3, 5, '#c73414']],
  },
  particle_soul_flame: {
    size: 8,
    pixels: [[3, 1, '#d9ffff'], [3, 2, '#6df0f7'], [4, 2, '#3ac9d4'], [2, 3, '#3ac9d4'], [3, 3, '#d9ffff'], [4, 3, '#229fa8'], [2, 4, '#197f87'], [3, 4, '#6df0f7'], [4, 4, '#197f87'], [3, 5, '#125f66']],
  },
  particle_glow: {
    size: 8,
    pixels: [[3, 1, '#a6f3ff'], [3, 2, '#d9fbff'], [1, 3, '#a6f3ff'], [2, 3, '#d9fbff'], [3, 3, '#ffffff'], [4, 3, '#d9fbff'], [5, 3, '#a6f3ff'], [3, 4, '#d9fbff'], [3, 5, '#a6f3ff']],
  },
  particle_glint: {
    size: 8,
    pixels: [[3, 1, '#e8c4ff'], [3, 2, '#f5e3ff'], [2, 3, '#e8c4ff'], [3, 3, '#ffffff'], [4, 3, '#e8c4ff'], [3, 4, '#f5e3ff'], [3, 5, '#c894f5']],
  },
  particle_spark_0: {
    size: 8,
    pixels: [[3, 2, '#ffffff'], [2, 3, '#e6f2fa'], [3, 3, '#ffffff'], [4, 3, '#e6f2fa'], [3, 4, '#ffffff']],
  },
  particle_spark_1: {
    size: 8,
    pixels: [[3, 3, '#ffffff'], [4, 3, '#d9ecfa'], [3, 4, '#d9ecfa']],
  },
}

export function proceduralTileDataUrl(key: string): string {
  const cached = proceduralCache.get(key)
  if (cached) return cached
  const particle = pixelParticleSprites[key]
  if (particle) {
    let rects = ''
    for (const [x, y, color] of particle.pixels) {
      rects += `<rect x="${x}" y="${y}" width="1" height="1" fill="${color}"/>`
    }
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${particle.size}" height="${particle.size}" viewBox="0 0 ${particle.size} ${particle.size}" shape-rendering="crispEdges">${rects}</svg>`
    const url = `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`
    proceduralCache.set(key, url)
    return url
  }
  const spec = tileSpecs[key] || tileSpecs.stone
  const seed = keySeed(key)
  let rects = `<rect width="16" height="16" fill="${spec.base}"/>`
  for (let y = 0; y < 16; y++) {
    for (let x = 0; x < 16; x++) {
      const h = hashCoord(seed, x, y)
      for (const [color, mod] of spec.specks) {
        if (h % (mod * 3) === 0) {
          rects += `<rect x="${x}" y="${y}" width="1" height="1" fill="${color}"/>`
          break
        }
      }
    }
  }
  if (spec.grid) {
    for (let y = 0; y < 16; y += spec.grid.stepY) {
      rects += `<rect x="0" y="${y}" width="16" height="1" fill="${spec.grid.color}" opacity="0.55"/>`
    }
    if (spec.grid.stepX < 16) {
      for (let x = 0; x < 16; x += spec.grid.stepX) {
        rects += `<rect x="${x}" y="0" width="1" height="16" fill="${spec.grid.color}" opacity="0.45"/>`
      }
    }
  }
  if (spec.bandTop) {
    rects += `<rect x="0" y="0" width="16" height="${spec.bandTop.height}" fill="${spec.bandTop.color}"/>`
    if (spec.bandTop.dripColor) {
      for (let x = 0; x < 16; x++) {
        const drop = hashCoord(seed + 17, x, 9) % 3
        if (drop > 0) {
          rects += `<rect x="${x}" y="${spec.bandTop.height}" width="1" height="${drop}" fill="${spec.bandTop.dripColor}"/>`
        }
      }
    }
  }
  if (spec.accentPixels) {
    for (const [x, y, color] of spec.accentPixels) {
      rects += `<rect x="${x}" y="${y}" width="1" height="1" fill="${color}"/>`
    }
  }
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" shape-rendering="crispEdges">${rects}</svg>`
  const url = `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`
  proceduralCache.set(key, url)
  return url
}

export function resolveTextureUrl(key: string, assets?: PublicThemeAssets | null): string {
  if (assets?.version && assets.keys?.includes(key)) {
    return `/api/theme-assets/${encodeURIComponent(assets.version)}/${encodeURIComponent(key)}.png`
  }
  return proceduralTileDataUrl(key)
}

const cssVars = [
  '--mc-tex-trim',
  '--mc-tex-footer',
  '--mc-scene-active',
  '--mc-scene-light',
  '--mc-scene-dark',
] as const

export function applyThemeTextures(pack: string, dark: boolean, assets?: PublicThemeAssets | null) {
  const root = document.documentElement
  const meta = themePackById[pack]
  if (!meta?.mcBiome || !meta.textures) {
    delete root.dataset.mcBiome
    for (const name of cssVars) root.style.removeProperty(name)
    return
  }
  root.dataset.mcBiome = '1'
  const slots = meta.textures
  const setUrl = (prop: string, key: string) => root.style.setProperty(prop, `url("${resolveTextureUrl(key, assets)}")`)
  setUrl('--mc-tex-trim', dark ? slots.darkTrim : slots.lightTrim)
  setUrl('--mc-tex-footer', dark ? slots.darkFooter : slots.lightFooter)
  if (meta.scenes) {
    const activeScene = dark ? meta.scenes.dark : (meta.scenes.light || meta.scenes.dark)
    root.style.setProperty('--mc-scene-active', `url("${activeScene}")`)
    root.style.setProperty('--mc-scene-light', `url("${meta.scenes.light || meta.scenes.dark}")`)
    root.style.setProperty('--mc-scene-dark', `url("${meta.scenes.dark}")`)
  }
}
