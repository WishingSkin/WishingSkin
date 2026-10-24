export async function imageData(event: Event): Promise<string> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return ''
  if (!['image/png', 'image/jpeg'].includes(file.type) || file.size > 512 * 1024) throw new Error('请选择不超过 512 KiB、2048×2048 的 PNG 或 JPEG 图片')
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(new Error('图片读取失败，请重新选择'))
    reader.readAsDataURL(file)
  })
}
