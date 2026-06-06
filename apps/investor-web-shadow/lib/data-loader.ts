import fs from 'fs'
import path from 'path'
import Papa from 'papaparse'
import type { CountingRecord, DailySummaryRecord, FeedLoadRecord, SeasonRecord } from './types'

const countingCache: Map<string, CountingRecord[]> = new Map()
const dailySummaryCache: Map<string, DailySummaryRecord[]> = new Map()
const feedCache: Map<string, FeedLoadRecord[]> = new Map()
const seasonsCache: Map<string, SeasonRecord[]> = new Map()

const DATA_DIR = path.join(process.cwd(), 'public', 'data')

const FEED_NUMERIC_FIELDS = [
  'load_id',
  'total_purchased_qty',
  'consumption_per_day',
  'current_stock',
  'days_stock_can_last',
  'days_consumed_so_far',
  'farm_load_total_consumed_qty',
  'last_load_total_cost',
  'paid_so_far',
  'pending_amount',
  'last_payment_amount',
  'purchase_cost',
  'transport_cost',
] as const

const COUNTING_NUMERIC_FIELDS = ['goat_count'] as const
const DAILY_SUMMARY_NUMERIC_FIELDS = [
  'farm_total_count',
  'farm_total_weight',
  'farm_total_weight_value',
  'cbe_summary_count',
  'cbe_summary_weight',
  'cbe_summary_weight_value',
  'cpt_summary_count',
  'cpt_summary_weight',
  'cpt_summary_weight_value',
  'procurement_summary_count',
  'procurement_summary_weight',
  'procurement_summary_weight_value',
  'adults_total_count',
  'adults_total_weight',
  'adults_total_weight_value',
  'cbe_adults_count',
  'cbe_adults_weight',
  'cbe_adults_weight_value',
  'cpt_adults_count',
  'cpt_adults_weight',
  'cpt_adults_weight_value',
  'kids_total_count',
  'kids_total_weight',
  'kids_total_weight_value',
  'cbe_kids_count',
  'cbe_kids_weight',
  'cbe_kids_weight_value',
  'cpt_kids_count',
  'cpt_kids_weight',
  'cpt_kids_weight_value',
] as const

function parseCsv<T>(
  filePath: string,
  numericFields: readonly string[]
): T[] {
  const csv = fs.readFileSync(filePath, 'utf-8')
  const result = Papa.parse<Record<string, string>>(csv, {
    header: true,
    skipEmptyLines: true,
  })

  return result.data.map((row) => {
    const converted: Record<string, unknown> = {}
    for (const key of Object.keys(row)) {
      if (numericFields.includes(key)) {
        converted[key] = Number(row[key]) || 0
      } else {
        converted[key] = row[key]
      }
    }
    return converted as T
  })
}

export async function getCountingData(): Promise<CountingRecord[]> {
  const cacheKey = 'counting'
  if (countingCache.has(cacheKey)) {
    return countingCache.get(cacheKey)!
  }

  const filePath = path.join(DATA_DIR, 'counting_db_with_holding_dev.csv')
  const data = parseCsv<CountingRecord>(filePath, COUNTING_NUMERIC_FIELDS)
  countingCache.set(cacheKey, data)
  return data
}

export async function getDailySummary(): Promise<DailySummaryRecord[]> {
  const cacheKey = 'dailySummary'
  if (dailySummaryCache.has(cacheKey)) {
    return dailySummaryCache.get(cacheKey)!
  }

  const filePath = path.join(DATA_DIR, 'daily_summary_dev.csv')
  const data = parseCsv<DailySummaryRecord>(filePath, DAILY_SUMMARY_NUMERIC_FIELDS)
  dailySummaryCache.set(cacheKey, data)
  return data
}

export async function getFeedData(): Promise<FeedLoadRecord[]> {
  const cacheKey = 'feed'
  if (feedCache.has(cacheKey)) {
    return feedCache.get(cacheKey)!
  }

  const filePath = path.join(DATA_DIR, 'last_10_loads_feedwise.csv')
  const data = parseCsv<FeedLoadRecord>(filePath, FEED_NUMERIC_FIELDS)
  feedCache.set(cacheKey, data)
  return data
}

export async function getSeasonsData(): Promise<SeasonRecord[]> {
  const cacheKey = 'seasons'
  if (seasonsCache.has(cacheKey)) {
    return seasonsCache.get(cacheKey)!
  }

  const filePath = path.join(DATA_DIR, 'seasons.csv')
  const data = parseCsv<SeasonRecord>(filePath, [])
  seasonsCache.set(cacheKey, data)
  return data
}
