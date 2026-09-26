import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { sexSentenceKey, type SexChoice } from "./sex-filter";

/** The farm's genders, in the order Configuration lists them (the `weights_sexes` group). */
export function weightsSexChoices(pageContract: AdminUiPageContract): SexChoice[] {
  return optionGroup(pageContract, "weights_sexes").map((option) => ({ value: option.key, label: option.label }));
}

/** A sentence naming the kids the page counted; see sexSentenceKey. */
export function sexSentence(
  pageContract: AdminUiPageContract,
  sexFilter: string,
  keys: { all: string; perSex: (sex: string) => string },
  choices: readonly SexChoice[],
): string {
  const { key, sex } = sexSentenceKey(sexFilter, keys, choices);
  return copy(pageContract, key).replaceAll("{sex}", sex);
}
