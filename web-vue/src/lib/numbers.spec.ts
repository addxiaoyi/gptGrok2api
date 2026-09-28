import { describe, expect, it } from 'vitest'
import {
  clampInt,
  clampNumber,
  toNonNegativeInt,
  toOptionalNonNegativeInt,
} from '@/lib/numbers'

describe('toNonNegativeInt', () => {
  it('接受数字类型', () => {
    expect(toNonNegativeInt(7)).toBe(7)
    expect(toNonNegativeInt(0)).toBe(0)
  })

  it('接受可转换数字的字符串', () => {
    expect(toNonNegativeInt('7')).toBe(7)
    expect(toNonNegativeInt('')).toBe(0)
    expect(toNonNegativeInt('abc')).toBe(0)
  })

  it('负数归零', () => {
    expect(toNonNegativeInt(-3)).toBe(0)
    expect(toNonNegativeInt(-0.5)).toBe(0)
  })

  it('截断小数', () => {
    expect(toNonNegativeInt(4.9)).toBe(4)
    expect(toNonNegativeInt(4.1)).toBe(4)
  })

  it('非有限值使用 fallback', () => {
    expect(toNonNegativeInt(NaN)).toBe(0)
    expect(toNonNegativeInt(Infinity)).toBe(0)
    expect(toNonNegativeInt(null)).toBe(0)
    expect(toNonNegativeInt(undefined)).toBe(0)
  })

  it('自定义 fallback', () => {
    expect(toNonNegativeInt('bad', 5)).toBe(5)
    expect(toNonNegativeInt(9, 5)).toBe(9)
  })
})

describe('toOptionalNonNegativeInt', () => {
  it('null/undefined/空串返回 undefined', () => {
    expect(toOptionalNonNegativeInt(null)).toBeUndefined()
    expect(toOptionalNonNegativeInt(undefined)).toBeUndefined()
    expect(toOptionalNonNegativeInt('')).toBeUndefined()
  })

  it('合法值返回数字', () => {
    expect(toOptionalNonNegativeInt(3)).toBe(3)
    expect(toOptionalNonNegativeInt('12')).toBe(12)
  })

  it('负数归零', () => {
    expect(toOptionalNonNegativeInt(-1)).toBe(0)
  })

  it('无法解析返回 undefined', () => {
    expect(toOptionalNonNegativeInt('xx')).toBeUndefined()
    expect(toOptionalNonNegativeInt(NaN)).toBeUndefined()
  })
})

describe('clampNumber', () => {
  it('区间内原样返回', () => {
    expect(clampNumber(5, 0, 10)).toBe(5)
  })

  it('越下界取下界', () => {
    expect(clampNumber(-1, 0, 10)).toBe(0)
  })

  it('越上界取上界', () => {
    expect(clampNumber(99, 0, 10)).toBe(10)
  })
})

describe('clampInt', () => {
  it('接受 unknown 并截断取整', () => {
    expect(clampInt('7.8', 0, 100)).toBe(7)
    expect(clampInt(-4, 0, 100)).toBe(0)
    expect(clampInt(500, 0, 100)).toBe(100)
  })

  it('默认 fallback 为下界', () => {
    expect(clampInt('bad', 5, 10)).toBe(5)
  })

  it('自定义 fallback 也会被 clamp 到范围内', () => {
    expect(clampInt('bad', 0, 10, 42)).toBe(10)
  })

  it('Infinity 使用 fallback', () => {
    expect(clampInt(Infinity, 0, 10, 3)).toBe(3)
  })
})