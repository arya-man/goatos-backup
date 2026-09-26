import { optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export type FormSelectOption = { value: string; label: string };

/**
 * Contract option group -> the `{value,label}` shape the MUI select options take.
 *
 * Presentation only: the keys and labels stay backend-owned, nothing is composed here. An
 * optional `placeholder` becomes the empty-value first entry the native `<select>` used to
 * carry as `<option value="" disabled>`.
 */
export function contractOptions(
  pageContract: AdminUiPageContract,
  groupId: string,
  placeholder?: string,
): FormSelectOption[] {
  const options = optionGroup(pageContract, groupId).map((option) => ({ value: option.key, label: option.label }));
  return placeholder === undefined ? options : [{ value: "", label: placeholder }, ...options];
}

/** Same shape, from an already-resolved list. */
export function listOptions<T>(
  rows: readonly T[],
  value: (row: T) => string,
  label: (row: T) => string,
  placeholder?: string,
): FormSelectOption[] {
  const options = rows.map((row) => ({ value: value(row), label: label(row) }));
  return placeholder === undefined ? options : [{ value: "", label: placeholder }, ...options];
}
