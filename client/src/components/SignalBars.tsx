// Connection quality from a round trip time: 4 bars under 60 ms, 3 under
// 120, 2 under 200, 1 under 300, red beyond (ms shown on hover).
export function pingLevel(rtt?: number) {
  if (rtt === undefined) return -1
  return rtt < 60 ? 4 : rtt < 120 ? 3 : rtt < 200 ? 2 : rtt < 300 ? 1 : 0
}

export function SignalBars({ rtt, label = 'Ping', size = 16 }: { rtt?: number; label?: string; size?: number }) {
  const level = pingLevel(rtt)
  const text = rtt === undefined ? label + ' : mesure…' : label + ' : ' + rtt + ' ms'
  const cls = level <= 0 ? 'bad' : level === 1 ? 'poor' : level === 2 ? 'fair' : 'good'
  return (
    <span className={'signal ' + cls} title={text} aria-label={text} role="img" data-rtt={rtt ?? ''}>
      <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true">
        {[0, 1, 2, 3].map((i) => (
          <rect key={i} x={1 + i * 4} y={12 - i * 3.3} width="2.6" height={3 + i * 3.3} rx="0.8" className={i < Math.max(level, 0) ? 'on' : 'off'} />
        ))}
      </svg>
    </span>
  )
}
