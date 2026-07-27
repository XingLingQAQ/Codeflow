/** Human relative time, tolerant of unix-seconds, unix-ms, or ISO strings. */
export function relTime(input?: number | string): string {
  if (input == null || input === '') return '';
  let ms = typeof input === 'string' ? Date.parse(input) : input;
  if (typeof input === 'number' && input < 1e12) ms = input * 1000;
  if (!Number.isFinite(ms)) return '';
  const diff = Date.now() - ms;
  const s = Math.round(diff / 1000);
  if (s < 45) return '刚刚';
  const m = Math.round(s / 60);
  if (m < 60) return `${m} 分钟前`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} 小时前`;
  const d = Math.round(h / 24);
  if (d < 30) return `${d} 天前`;
  return new Date(ms).toLocaleDateString();
}

export function greeting(): string {
  const h = new Date().getHours();
  if (h < 5) return '夜深了';
  if (h < 12) return '早上好';
  if (h < 18) return '下午好';
  return '晚上好';
}
