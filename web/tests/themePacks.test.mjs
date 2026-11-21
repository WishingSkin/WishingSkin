import test from 'node:test'
import assert from 'node:assert/strict'
import { allThemePacks, isDarkOnlyTheme, isMCBiomeTheme, isMCFamilyTheme, themePackById } from '../src/themePacks.ts'
import { proceduralTileDataUrl, resolveTextureUrl } from '../src/themeTextures.ts'

test('theme packs expose 14 paired themes with scenes and pixel particle profiles', () => {
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
      assert.ok(pack.scenes?.dark, `${pack.value} should define dark scenic background`)
      if (!pack.darkOnly) {
        assert.ok(pack.scenes?.light, `${pack.value} should define light scenic background`)
      }
      assert.ok(pack.particles?.dark, `${pack.value} should define dark pixel particle profile`)
    }
  }
  assert.ok(themePackById.high_contrast)
})

test('resolveTextureUrl prefers imported jar assets and falls back to procedural SVG tiles and pixel particles', () => {
  const builtin = resolveTextureUrl('plains_grass_side', null)
  assert.ok(builtin.startsWith('data:image/svg+xml;utf8,'))
  assert.equal(builtin, proceduralTileDataUrl('plains_grass_side'))

  const builtinParticle = resolveTextureUrl('particle_cherry_0', null)
  assert.ok(builtinParticle.startsWith('data:image/svg+xml;utf8,'))
  assert.ok(decodeURIComponent(builtinParticle).includes('width="8"'))

  const imported = resolveTextureUrl('particle_cherry_0', {
    version: 'abc12345',
    count: 2,
    keys: ['plains_grass_side', 'particle_cherry_0'],
  })
  assert.equal(imported, '/api/theme-assets/abc12345/particle_cherry_0.png')

  const missingKey = resolveTextureUrl('ice_crystal', {
    version: 'abc12345',
    count: 2,
    keys: ['plains_grass_side', 'particle_cherry_0'],
  })
  assert.ok(missingKey.startsWith('data:image/svg+xml;utf8,'))
})
