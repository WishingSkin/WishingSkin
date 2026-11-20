import test from 'node:test'
import assert from 'node:assert/strict'
import { allThemePacks, isDarkOnlyTheme, isMCBiomeTheme, isMCFamilyTheme, themePackById } from '../src/themePacks.ts'
import { proceduralTileDataUrl, resolveTextureUrl } from '../src/themeTextures.ts'

test('theme packs expose 14 paired themes and mark deep_dark as dark-only', () => {
  assert.equal(allThemePacks.length, 14)
  assert.equal(isDarkOnlyTheme('deep_dark'), true)
  assert.equal(isDarkOnlyTheme('plains'), false)
  assert.equal(isMCBiomeTheme('snowy'), true)
  assert.equal(isMCBiomeTheme('mc_official'), false)
  assert.equal(isMCFamilyTheme('mc_official'), true)
  assert.equal(isMCFamilyTheme('mc_wiki'), true)
  assert.equal(isMCFamilyTheme('nord'), false)
  for (const pack of allThemePacks) {
    if (pack.mcBiome) {
      assert.ok(pack.textures, `${pack.value} should define texture slots`)
      assert.equal(pack.textures.strata.length, 4)
    }
  }
  assert.ok(themePackById.high_contrast)
})

test('resolveTextureUrl prefers imported jar assets and falls back to procedural SVG tiles', () => {
  const builtin = resolveTextureUrl('plains_grass_side', null)
  assert.ok(builtin.startsWith('data:image/svg+xml;utf8,'))
  assert.equal(builtin, proceduralTileDataUrl('plains_grass_side'))

  const imported = resolveTextureUrl('plains_grass_side', {
    version: 'abc12345',
    count: 2,
    keys: ['plains_grass_side', 'bedrock'],
  })
  assert.equal(imported, '/api/theme-assets/abc12345/plains_grass_side.png')

  const missingKey = resolveTextureUrl('ice_crystal', {
    version: 'abc12345',
    count: 2,
    keys: ['plains_grass_side', 'bedrock'],
  })
  assert.ok(missingKey.startsWith('data:image/svg+xml;utf8,'))
})
