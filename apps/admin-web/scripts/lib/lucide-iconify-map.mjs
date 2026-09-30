// guard: lucide-banned (FIXJ4, J1 P1-3).
//
// THE one lucide-react -> template Iconify mapping table. admin-web draws every icon with
// `<Iconify icon="..." />` from @/components/minimal/iconify (the Minimal template's icon set; solar
// first, eva/mingcute where the template itself uses them). lucide-react is no longer a dependency and
// design:guard `lucide-banned` refuses any import of it; its finding message names the replacement
// from this table. Every value must be registered in layouts/template/iconify/icon-sets.ts (guard
// iconify-offline-set; the Iconify `icon` prop is typed to the registered names).
//
// Size: lucide `size={n}` -> Iconify `width={n}`. `strokeWidth`, `className="ic"`, `aria-hidden` drop
// (Iconify renders aria-hidden). Spinners (Loader2) are MUI `<CircularProgress size={n} />`, not an icon.
export const LUCIDE_TO_ICONIFY = Object.freeze({
  AlertTriangle: "solar:danger-triangle-bold",
  ArrowLeft: "eva:arrow-ios-back-fill",
  ArrowRight: "eva:arrow-forward-fill",
  ArrowUpDown: "carbon:chevron-sort",
  AtSign: "solar:letter-bold",
  Bell: "solar:bell-bing-bold",
  BellOff: "solar:bell-off-bold",
  BellRing: "solar:bell-bing-bold-duotone",
  BookText: "solar:notebook-bold-duotone",
  Boxes: "solar:box-minimalistic-bold",
  Calendar: "solar:calendar-date-bold",
  CalendarClock: "solar:sort-by-time-bold-duotone",
  CalendarDays: "solar:calendar-date-bold",
  CalendarOff: "solar:forbidden-circle-bold",
  Camera: "solar:camera-add-bold",
  Check: "eva:checkmark-fill",
  CheckCheck: "eva:done-all-fill",
  CheckCircle2: "solar:check-circle-bold",
  ChevronDown: "eva:arrow-ios-downward-fill",
  ChevronLeft: "eva:arrow-ios-back-fill",
  ChevronRight: "eva:arrow-ios-forward-fill",
  CircleAlert: "solar:danger-bold",
  ClipboardList: "solar:bill-list-bold",
  Clock: "solar:clock-circle-bold",
  Columns3: "ic:round-view-module",
  Compass: "solar:home-angle-bold-duotone",
  Copy: "solar:copy-bold",
  Download: "solar:download-bold",
  ExternalLink: "eva:external-link-fill",
  Eye: "solar:eye-bold",
  FileCheck2: "solar:file-check-bold-duotone",
  FileText: "solar:file-text-bold",
  FlaskConical: "solar:atom-bold-duotone",
  Gavel: "solar:verified-check-bold",
  GitBranch: "solar:transfer-horizontal-bold-duotone",
  Hash: "solar:tag-horizontal-bold-duotone",
  HeartPulse: "solar:medical-kit-bold",
  Image: "solar:gallery-wide-bold",
  Inbox: "solar:inbox-bold",
  List: "solar:list-bold",
  ListChecks: "solar:bill-list-bold-duotone",
  Lock: "solar:lock-password-outline",
  LogOut: "ic:round-power-settings-new",
  MapPin: "mingcute:location-fill",
  MessageSquare: "solar:chat-round-dots-bold",
  MessageSquareText: "solar:chat-round-dots-bold",
  Mic: "solar:microphone-bold",
  Minus: "mingcute:minimize-line",
  Monitor: "solar:monitor-bold",
  Moon: "solar:cloudy-moon-bold-duotone",
  MousePointerClick: "eva:diagonal-arrow-left-down-fill",
  NotebookPen: "solar:notes-bold-duotone",
  Package: "solar:box-minimalistic-bold",
  PackageCheck: "solar:box-minimalistic-bold",
  Paperclip: "eva:attach-2-fill",
  Pencil: "solar:pen-bold",
  PencilLine: "solar:pen-bold",
  Plus: "mingcute:add-line",
  Radio: "ic:baseline-wifi",
  RotateCcw: "solar:restart-bold",
  Scale: "solar:dumbbell-large-minimalistic-bold",
  Scan: "carbon:fit-to-screen",
  ScanLine: "carbon:center-to-fit",
  Search: "eva:search-fill",
  Settings: "solar:settings-bold",
  ShieldAlert: "solar:shield-keyhole-bold-duotone",
  ShieldCheck: "solar:shield-check-bold",
  SlidersHorizontal: "ic:round-filter-list",
  Snowflake: "solar:ssd-round-bold",
  Sun: "solar:palette-bold-duotone",
  Syringe: "solar:medical-kit-bold",
  Trash2: "solar:trash-bin-trash-bold",
  TriangleAlert: "solar:danger-triangle-bold",
  Truck: "carbon:delivery",
  Type: "solar:file-text-bold",
  User: "solar:user-rounded-bold",
  UsersRound: "solar:users-group-rounded-bold",
  Video: "solar:videocamera-record-bold",
  Wheat: "custom:fast-food-fill",
  X: "mingcute:close-line",
});

export const LUCIDE_SPINNERS = Object.freeze(["Loader2", "LoaderCircle", "Loader"]);

export function iconifyFor(lucideName) {
  if (LUCIDE_SPINNERS.includes(lucideName)) return "<CircularProgress size={n} /> (MUI)";
  return LUCIDE_TO_ICONIFY[lucideName] ?? null;
}
