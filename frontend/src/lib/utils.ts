import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'
import { format, parseISO, isAfter, isBefore, addHours } from 'date-fns'
import { enGB } from 'date-fns/locale'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatDate(iso: string): string {
  return format(parseISO(iso), 'dd/MM/yyyy', { locale: enGB })
}

export function formatTime(iso: string): string {
  return format(parseISO(iso), 'HH:mm', { locale: enGB })
}

export function formatDateTime(iso: string): string {
  return format(parseISO(iso), 'dd/MM/yyyy HH:mm', { locale: enGB })
}

export function formatCurrency(pence: number): string {
  return `£${(pence / 100).toFixed(2)}`
}

export function isWithin24Hours(iso: string): boolean {
  const sessionTime = parseISO(iso)
  const now = new Date()
  const cutoff = addHours(now, 24)
  return isAfter(sessionTime, now) && isBefore(sessionTime, cutoff)
}

export const DAYS_OF_WEEK = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] as const
