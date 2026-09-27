export function toNonNegativeInt(value: unknown, fallback = 0): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? Math.max(0, Math.trunc(parsed)) : fallback
}

export function toOptionalNonNegativeInt(value: unknown): number | undefined {
  if (value === null || value === undefined || value === '') return undefined
  const parsed = Number(value)
  return Number.isFinite(parsed) ? Math.max(0, Math.trunc(parsed)) : undefined
}

export function clampNumber(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

export function clampInt(value: unknown, min: number, max: number, fallback = min): number {
  const parsed = Number(value)
  const finite = Number.isFinite(parsed) ? Math.trunc(parsed) : fallback
  return clampNumber(finite, min, max)
}
