import type { SkinViewer } from 'skinview3d'

export function defaultPreviewSkin(model: string) {
  return '/defaults/skins/' + model
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
    async function texture(url: string) {
      const response = await fetch(url, { signal, credentials: 'same-origin' })
      if (!response.ok) throw new Error('Texture unavailable')
      const bitmap = await createImageBitmap(await response.blob())
      bitmaps.push(bitmap)
      return bitmap
    }
    try {
      const { SkinViewer } = await import('skinview3d')
      const skin = await texture(skinHash ? '/textures/' + encodeURIComponent(skinHash) : defaultPreviewSkin(model))
      const cape = capeHash ? await texture('/textures/' + encodeURIComponent(capeHash)) : null
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
      viewer = new SkinViewer({ width: 180, height: 220, pixelRatio: 1, renderPaused: true, preserveDrawingBuffer: true, zoom: 0.85 })
      viewer.controls.enabled = false
      viewer.loadSkin(skin, { model })
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
