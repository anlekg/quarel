const time = new Intl.DateTimeFormat('fr-FR', { hour: '2-digit', minute: '2-digit' })
const day = new Intl.DateTimeFormat('fr-FR', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
const full = new Intl.DateTimeFormat('fr-FR', { dateStyle: 'full', timeStyle: 'short' })

export function formatTime(iso: string) {
  return time.format(new Date(iso))
}

export function formatFull(iso: string) {
  return full.format(new Date(iso))
}

// "Aujourd'hui à 14:02", "Hier à 09:15", or the date.
export function formatStamp(iso: string, now = new Date()) {
  const d = new Date(iso)
  const days = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000)
  if (days === 0) return 'Aujourd’hui à ' + time.format(d)
  if (days === 1) return 'Hier à ' + time.format(d)
  return d.toLocaleDateString('fr-FR') + ' à ' + time.format(d)
}

export function formatDay(iso: string) {
  const s = day.format(new Date(iso))
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function sameDay(a: string, b: string) {
  return startOfDay(new Date(a)) === startOfDay(new Date(b))
}

function startOfDay(d: Date) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

export function formatSize(bytes: number) {
  if (bytes < 1024) return bytes + ' o'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0).replace('.', ',') + ' Ko'
  return (bytes / 1024 / 1024).toFixed(1).replace('.', ',') + ' Mo'
}

export function roleColor(c: number) {
  return c ? '#' + c.toString(16).padStart(6, '0') : undefined
}
