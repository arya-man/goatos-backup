export const adminBasePath = "";

export const adminRoutes = {
  counts: "/counts",
  locations: "/locations",
  mortality: "/dashboard/mortality",
  operators: "/operators",
  sops: "/sops",
  tasks: "/tasks",
} as const;

export function formAction(path: string): string {
  return path;
}
