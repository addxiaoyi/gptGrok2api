import { describe, expect, it } from 'vitest'
import {
  formatDateTime,
  formatDateTimeFromSeconds,
  formatDateTimeFromSecondsSafe,
  formatTimestamp,
} from '@/lib/time'

describe('formatDateTime', () => {
  it('空输入返回 -', () => {
    expect(formatDateTime('')).toBe('-')
    expect(formatDateTime(null)).toBe('-')
    expect(formatDateTime(undefined)).toBe('-')
  })

  it('无效日期原样返回', () => {
    expect(formatDateTime('not-a-date')).toBe('not-a-date')
  })

  it('格式化 ISO 字符串（容忍本地分隔符）', () => {
    const result = formatDateTime('2024-01-01T09:05:00')
    // Intl.DateTimeFormat 可能输出 2024/01/01 或 2024-01-01
    expect(result).toMatch(/2024[\/\-]01[\/\-]01\s+\d{2}:\d{2}/)
  })
})

describe('formatTimestamp', () => {
  it('空输入返回 -', () => {
    expect(formatTimestamp('')).toBe('-')
    expect(formatTimestamp(null)).toBe('-')
  })

  it('秒级时间戳按本地时间显示', () => {
    const result = formatTimestamp('1704067500')
    // 1704067500 = 2024-01-01T01:45:00 UTC
    expect(result).toMatch(/2024/)
  })

  it('毫秒级时间戳按本地时间显示', () => {
    const result = formatTimestamp('1704067500000')
    expect(result).toMatch(/2024/)
  })

  it('无效值原样返回', () => {
    expect(formatTimestamp('garbage')).toBe('garbage')
  })
})

describe('formatDateTimeFromSeconds', () => {
  it('格式化秒级时间戳为 YYYY-MM-DD HH:mm', () => {
    const result = formatDateTimeFromSeconds(1704067500)
    const match = result.match(/^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})$/)
    expect(match).not.toBeNull()
  })

  it('0 秒返回 1970 年本地时间', () => {
    const result = formatDateTimeFromSeconds(0)
    expect(result).toMatch(/^1970-/)
  })
})

describe('formatDateTimeFromSecondsSafe', () => {
  it('非法/非正值返回 -', () => {
    expect(formatDateTimeFromSecondsSafe(undefined)).toBe('-')
    expect(formatDateTimeFromSecondsSafe(0)).toBe('-')
    expect(formatDateTimeFromSecondsSafe(-5)).toBe('-')
  })

  it('合法值委托给 formatDateTimeFromSeconds', () => {
    expect(formatDateTimeFromSecondsSafe(1704067500)).toBe(
      formatDateTimeFromSeconds(1704067500),
    )
  })
})