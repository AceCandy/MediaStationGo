// dominantColor 从同源图片中提取带饱和度权重的主色，用于影院式环境光晕。
// 图片走 /api/img 或 /api/artwork 同源代理，canvas 不会被污染；失败时静默返回 null。

const cache = new Map<string, Promise<string | null>>()

export function dominantColor(src: string): Promise<string | null> {
  if (!src) return Promise.resolve(null)
  let cached = cache.get(src)
  if (!cached) {
    cached = extract(src).catch(() => null)
    cache.set(src, cached)
  }
  return cached
}

async function extract(src: string): Promise<string | null> {
  const img = new Image()
  img.decoding = 'async'
  await new Promise<void>((resolve, reject) => {
    img.onload = () => resolve()
    img.onerror = () => reject(new Error('image load failed'))
    img.src = src
  })
  const size = 32
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  if (!ctx) return null
  ctx.drawImage(img, 0, 0, size, size)
  let data: Uint8ClampedArray
  try {
    data = ctx.getImageData(0, 0, size, size).data
  } catch {
    return null
  }
  let r = 0
  let g = 0
  let b = 0
  let weight = 0
  for (let i = 0; i < data.length; i += 4) {
    const pr = data[i]
    const pg = data[i + 1]
    const pb = data[i + 2]
    const max = Math.max(pr, pg, pb)
    const min = Math.min(pr, pg, pb)
    const saturation = max === 0 ? 0 : (max - min) / max
    // 跳过死黑与纯白像素，让结果偏向画面真正的色彩倾向
    if (max < 24 || (max > 242 && saturation < 0.08)) continue
    const w = 0.35 + saturation
    r += pr * w
    g += pg * w
    b += pb * w
    weight += w
  }
  if (weight === 0) return null
  return `${Math.round(r / weight)}, ${Math.round(g / weight)}, ${Math.round(b / weight)}`
}
