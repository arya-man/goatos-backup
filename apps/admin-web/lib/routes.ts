export const adminBasePath = "/dashboard";

export function formAction(path: string): string {
  if (!path.startsWith("/") || path.startsWith(adminBasePath)) {
    return path;
  }
  if (path === "/") {
    return adminBasePath;
  }
  return `${adminBasePath}${path}`;
}
