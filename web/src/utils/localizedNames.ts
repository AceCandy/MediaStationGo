const chineseNames = typeof Intl.DisplayNames === 'function'
  ? {
      region: new Intl.DisplayNames(['zh-CN'], { type: 'region' }),
      language: new Intl.DisplayNames(['zh-CN'], { type: 'language' }),
    }
  : null

export function localizedName(value: string, type: 'region' | 'language', fallback = value): string {
  const code = type === 'region' ? value.toUpperCase() : value
  const isCode = type === 'region'
    ? /^(?:[A-Z]{2}|\d{3})$/.test(code)
    : /^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$/.test(code)
  if (!isCode || !chineseNames) return fallback
  try {
    const label = chineseNames[type].of(code)
    return label && label !== code ? label : fallback
  } catch {
    return fallback
  }
}
