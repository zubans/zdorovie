export function parseSummarySections(text) {
  if (!text || typeof text !== 'string') return []
  const lines = text.split(/\r?\n/)
  const sections = []
  let current = { heading: '', items: [], paragraphs: [] }

  const flush = () => {
    if (current.heading || current.items.length || current.paragraphs.length) {
      sections.push(current)
      current = { heading: '', items: [], paragraphs: [] }
    }
  }

  for (const raw of lines) {
    const line = raw.trim()
    if (!line) continue
    const mdHeading = line.match(/^#{1,4}\s+(.+)$/)
    const colonHeading = !line.startsWith('-') && !line.startsWith('•') && line.match(/^(\*{0,2})([А-ЯA-ZЁ][^:]{2,55}):(\*{0,2})\s*(.*)$/)

    if (mdHeading) {
      flush()
      current.heading = mdHeading[1].replace(/\*\*/g, '').trim()
      continue
    }
    if (colonHeading) {
      const label = colonHeading[2].trim()
      const tail = (colonHeading[4] || '').trim()
      if (!tail || label.length <= 42) {
        flush()
        current.heading = label
        if (tail) current.paragraphs.push(tail.replace(/\*\*/g, ''))
        continue
      }
    }

    const bullet = line.match(/^([-*•]|\d+[.)])\s+(.+)$/)
    if (bullet) {
      current.items.push(bullet[2].replace(/\*\*/g, '').trim())
      continue
    }
    current.paragraphs.push(line.replace(/\*\*/g, '').trim())
  }
  flush()
  return sections
}

export function getInitials(name) {
  const parts = String(name || '')
    .trim()
    .split(/\s+/)
    .filter(Boolean)
  if (parts.length === 0) return 'АВ'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}
