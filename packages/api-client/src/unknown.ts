/**
 * §12 P0：无法确认状态时不得显示确定的 Running/Failed/0 quota。
 * 这里集中做"显示层不猜测"，视图不许自己写三元判断。
 */
export const UNKNOWN = '未知';

/** 0 是真实观测值，必须显示 0；只有 null/undefined/NaN 才是未知 */
export function num(v: number | null | undefined, unit = ''): string {
  return typeof v === 'number' && Number.isFinite(v) ? `${v}${unit}` : UNKNOWN;
}

export function pct(v: number | null | undefined): string {
  return num(v, '%');
}

/** 值本身有、但置信度是 UNKNOWN → 仍显示未知（不得替 Core 猜） */
export function observed(v: number | null | undefined, confidence: string | null | undefined,
  unit = ''): string {
  return confidence === 'UNKNOWN' ? UNKNOWN : num(v, unit);
}

/** 枚举原样显示；UNKNOWN 与 UNAVAILABLE/STOPPED/FAILED 各自独立，绝不合并 */
export function statusText(v: string | null | undefined): string {
  return v === null || v === undefined || v === 'UNKNOWN' ? UNKNOWN : v;
}

export function when(ts: number | null | undefined): string {
  return typeof ts === 'number' && Number.isFinite(ts) ? new Date(ts).toLocaleTimeString() : UNKNOWN;
}
