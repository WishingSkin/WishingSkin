import type { SkinViewer } from 'skinview3d'

// Locally drawn Steve-style colors; no downloaded or user sample asset is shipped.
export function defaultPreviewSkin(model: string) {
  const canvas = document.createElement('canvas')
  canvas.width = canvas.height = 64
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('Unable to create preview skin')
  function fill(x: number, y: number, width: number, height: number, color: string) {
    ctx!.fillStyle = color; ctx!.fillRect(x, y, width, height)
  }
  function part(x: number, y: number, width: number, height: number, depth: number, color: string) {
    fill(x + depth, y, width * 2, depth, color)
    fill(x, y + depth, width * 2 + depth * 2, height, color)
  }
  const arm = model === 'slim' ? 3 : 4
  part(0, 0, 8, 8, 8, '#b98b6b')
  fill(8, 0, 16, 8, '#392a20'); fill(0, 8, 8, 8, '#392a20'); fill(24, 8, 8, 8, '#392a20')
  fill(8, 8, 8, 2, '#392a20'); fill(16, 8, 8, 3, '#392a20')
  fill(9, 11, 2, 1, '#f5f0dd'); fill(13, 11, 2, 1, '#f5f0dd')
  fill(10, 11, 1, 1, '#4e4b8f'); fill(13, 11, 1, 1, '#4e4b8f')
  fill(11, 13, 2, 1, '#93664c'); fill(10, 14, 4, 1, '#614634')
  part(16, 16, 8, 12, 4, '#22aeb0'); fill(23, 20, 2, 2, '#b98b6b')
  for (const [x, y] of [[40, 16], [32, 48]]) {
    part(x!, y!, arm, 12, 4, '#b98b6b')
    fill(x!, y! + 4, arm * 2 + 8, 4, '#22aeb0')
  }
  for (const [x, y] of [[0, 16], [16, 48]]) {
    part(x!, y!, 4, 12, 4, '#494393'); fill(x!, y! + 14, 16, 2, '#3a393e')
  }
  return canvas
}

export function disposePreview(viewer: SkinViewer) {
  const resources = new Set<{ dispose(): void }>()
  viewer.scene.traverse(object => {
    const mesh = object as typeof object & { geometry?: { dispose(): void }; material?: { dispose(): void } | { dispose(): void }[] }
    if (mesh.geometry) resources.add(mesh.geometry)
    if (mesh.material) for (const material of Array.isArray(mesh.material) ? mesh.material : [mesh.material]) resources.add(material)
  })
  for (const resource of resources) resource.dispose()
  viewer.composer.dispose()
  viewer.fxaaPass.material.dispose()
  viewer.dispose()
  viewer.renderer.forceContextLoss()
}

let thumbnails: Promise<unknown> = Promise.resolve()

export function modelThumbnail(skinHash: string, capeHash: string, model: string, signal: AbortSignal) {
  const render = thumbnails.then(async () => {
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
    if (model !== 'default' && model !== 'slim') throw new Error('Unsupported model')
    const bitmaps: ImageBitmap[] = []
    let viewer: SkinViewer | undefined
    async function texture(hash: string) {
      const response = await fetch('/textures/' + encodeURIComponent(hash), { signal, credentials: 'same-origin' })
      if (!response.ok) throw new Error('Texture unavailable')
      const bitmap = await createImageBitmap(await response.blob())
      bitmaps.push(bitmap)
      return bitmap
    }
    try {
      const { SkinViewer } = await import('skinview3d')
      const skin = skinHash ? await texture(skinHash) : defaultPreviewSkin(model)
      const cape = capeHash ? await texture(capeHash) : null
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
      viewer = new SkinViewer({ width: 180, height: 220, pixelRatio: 1, renderPaused: true, preserveDrawingBuffer: true, zoom: 0.85 })
      viewer.controls.enabled = false
      viewer.loadSkin(skin, { model })
      if (!skinHash) viewer.playerObject.skin.setOuterLayerVisible(false)
      if (cape) viewer.loadCape(cape, { backEquipment: 'cape' })
      const distance = viewer.camera.position.length()
      viewer.camera.position.set(distance * 0.35, distance * 0.08, distance * (cape && !skinHash ? -0.93 : 0.93)).setLength(distance)
      viewer.camera.lookAt(0, 0, 0)
      viewer.render()
      return viewer.canvas.toDataURL('image/png')
    } finally {
      if (viewer) disposePreview(viewer)
      for (const bitmap of bitmaps) bitmap.close()
    }
  })
  thumbnails = render.catch(() => {})
  return render
}
