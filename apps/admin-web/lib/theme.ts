import { storageKey } from "@/lib/brand";

export const THEME_STORAGE_KEY = storageKey("shell", "theme");
export type ThemeMode = "light" | "dark";

export const THEME_BOOT_SCRIPT = `(function(){try{var m=null;try{m=localStorage.getItem("${THEME_STORAGE_KEY}")}catch(e){}if(m!=="light"&&m!=="dark"){m="dark"}var r=document.documentElement;r.classList.toggle("light",m==="light");r.classList.toggle("dark",m==="dark");r.setAttribute("data-theme",m);r.style.colorScheme=m}catch(e){}})();`;

export function applyTheme(mode: ThemeMode) {
  const root = document.documentElement;
  root.classList.add("theme-switching");
  root.classList.toggle("light", mode === "light");
  root.classList.toggle("dark", mode === "dark");
  root.setAttribute("data-theme", mode);
  root.style.colorScheme = mode;
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, mode);
  } catch {
    /* storage unavailable: the choice lasts for this page only */
  }
  window.setTimeout(() => root.classList.remove("theme-switching"), 320);
}

export function readTheme(): ThemeMode {
  if (typeof document === "undefined") return "dark";
  return document.documentElement.classList.contains("light") ? "light" : "dark";
}
