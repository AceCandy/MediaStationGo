import { useEffect, useState } from 'react'

import { dominantColor } from '../utils/dominantColor'

// useDominantColor 返回图片主色（"r, g, b" 格式），未就绪或失败时为 null。
export function useDominantColor(src: string | undefined | null): string | null {
  const [color, setColor] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setColor(null)
    if (!src) return undefined
    void dominantColor(src).then((value) => {
      if (!cancelled) setColor(value)
    })
    return () => { cancelled = true }
  }, [src])

  return color
}
