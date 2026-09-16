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
export type ModelGroups = Record<string, string[]>

export function parseModelGroups(
  value: string | undefined
): ModelGroups | null {
  if (!value?.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return null
    }
    if (
      !Object.values(parsed).every(
        (groups) =>
          Array.isArray(groups) &&
          groups.every((group) => typeof group === 'string')
      )
    ) {
      return null
    }
    return parsed as ModelGroups
  } catch {
    return null
  }
}

export function validateModelGroups(
  value: string | undefined,
  models: string[],
  groups: string[]
): string | null {
  const bindings = parseModelGroups(value)
  if (!bindings) return 'Model groups must be a JSON object with string arrays'
  const modelSet = new Set(models)
  const groupSet = new Set(groups)
  const encoder = new TextEncoder()
  for (const [model, boundGroups] of Object.entries(bindings)) {
    if (
      !modelSet.has(model) ||
      model.trim() !== model ||
      encoder.encode(model).length > 255
    ) {
      return 'Model group bindings must reference models in this channel'
    }
    if (boundGroups.length === 0) {
      return 'Select at least one group for each model, or use channel groups'
    }
    if (
      new Set(boundGroups).size !== boundGroups.length ||
      boundGroups.some(
        (group) =>
          !group ||
          group.trim() !== group ||
          group.includes(',') ||
          encoder.encode(group).length > 64 ||
          !groupSet.has(group)
      )
    ) {
      return 'Model group bindings must use groups selected for this channel'
    }
  }
  return null
}
