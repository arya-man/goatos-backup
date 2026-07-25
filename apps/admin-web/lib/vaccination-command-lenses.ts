import { revalidatePath } from "next/cache";

export const VACCINATION_COMMAND_LENS_PATHS = [
  "/vaccination",
  "/vaccination/execution",
  "/calendar",
  "/action-center",
  "/protocol-adherence",
  "/workflows",
  "/",
] as const;

export function revalidateVaccinationCommandLenses(): void {
  for (const path of VACCINATION_COMMAND_LENS_PATHS) {
    revalidatePath(path);
  }
  revalidatePath("/vaccination/execution/sheds/[shedId]", "page");
}
