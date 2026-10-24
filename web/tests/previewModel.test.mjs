import assert from 'node:assert/strict'
import { test } from 'node:test'
import { defaultPreviewSkin } from '../src/components/previewModel.ts'

for (const [model, name] of [['default', 'Steve'], ['slim', 'Alex']]) {
  test(`${name} previews load the same built-in skin as game profiles`, () => {
    assert.equal(defaultPreviewSkin(model), `/defaults/skins/${model}`)
  })
}
