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

/**
 * 额度三档的分类（T-449 ②）—— **只分类，不写文案**，文案留给视图，这样三档的差异不会在某处被折叠成一个数字：
 * - `value`：有数且置信度够（`>0` 高置信是典型），正常显示数字
 * - `zero`：**值是 0 且置信不足**（`confidence !== 'HIGH'`）—— 这一档既不能读成"用完"也不能读成"免费"
 * - `absent`：读不到（null / NaN / `confidence: UNKNOWN`）—— 不产出任何数字
 */
export function quotaTier(v: number | null | undefined, confidence: string | null | undefined):
  'value' | 'zero' | 'absent' {
  if (!(typeof v === 'number' && Number.isFinite(v)) || confidence === 'UNKNOWN') return 'absent';
  if (v === 0 && confidence !== 'HIGH') return 'zero';
  return 'value';
}
