/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export type ModelScheduleWindow = {
  weekday_mask: number
  start_minute: number
  end_minute: number
}

export type ModelSchedules = Record<string, ModelScheduleWindow[]>

export const MAX_MODEL_SCHEDULE_WINDOWS = 64

export function parseModelSchedules(
  value: string | undefined
): ModelSchedules | null {
  if (!value?.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return null
    }
    if (
      !Object.values(parsed).every(
        (windows) =>
          Array.isArray(windows) &&
          windows.every(
            (window) =>
              window &&
              typeof window === 'object' &&
              !Array.isArray(window) &&
              typeof window.weekday_mask === 'number' &&
              typeof window.start_minute === 'number' &&
              typeof window.end_minute === 'number'
          )
      )
    ) {
      return null
    }
    return parsed as ModelSchedules
  } catch {
    return null
  }
}

export function validateModelSchedules(
  value: string | undefined
): string | null {
  const schedules = parseModelSchedules(value)
  if (!schedules) {
    return 'Model schedules must be a JSON object containing arrays of time windows'
  }
  for (const [model, windows] of Object.entries(schedules)) {
    if (!model || model.trim() !== model) {
      return 'Model schedule keys must be non-empty model names without surrounding spaces'
    }
    if (windows.length > MAX_MODEL_SCHEDULE_WINDOWS) {
      return 'Each model supports at most 64 time windows'
    }
    for (const [index, window] of windows.entries()) {
      if (
        !Number.isInteger(window.weekday_mask) ||
        window.weekday_mask < 1 ||
        window.weekday_mask > 127
      ) {
        return 'Select at least one weekday for each time window'
      }
      if (
        !Number.isInteger(window.start_minute) ||
        !Number.isInteger(window.end_minute) ||
        window.start_minute < 0 ||
        window.start_minute >= 1440 ||
        window.end_minute < 1 ||
        window.end_minute > 1440 ||
        window.end_minute <= window.start_minute
      ) {
        return 'End time must be after start time within the same day; split overnight windows into two'
      }
      if (
        windows
          .slice(0, index)
          .some(
            (other) =>
              (other.weekday_mask & window.weekday_mask) !== 0 &&
              other.start_minute < window.end_minute &&
              window.start_minute < other.end_minute
          )
      ) {
        return 'Time windows for the same model cannot overlap on the same weekday'
      }
    }
  }
  return null
}
