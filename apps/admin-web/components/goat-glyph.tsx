import type { SVGProps } from "react";

/**
 * The Mesha goat as a FILLED side-profile silhouette (currentColor, one path, 24-grid): standing
 * body, four legs, short upright tail, head to the left with a beard under the chin and two horns
 * curving back over the neck. A filled shape stays a goat at 24px in an IconBadge and at 96px as
 * a KPI watermark; line art did not. Use it for animals / herd / head counts / sold animals.
 * The colourful Twemoji goat (features/ceo-ai/goat-twemoji.svg) stays the assistant's mascot.
 */
export const GOAT_PATH =
  "M2.6 6.6 C1.6 7.2 0.9 8.2 1.2 9.2 L2.8 9.9 L3.2 12.7 L4.5 10.2 C5.4 10.5 6.2 10.9 6.8 11.7 L7.5 13.0 C7.9 13.5 8.4 13.7 9.0 13.7 L8.5 21.8 L10.1 21.8 L10.4 15.9 L11.2 15.9 L11.5 21.8 L13.1 21.8 L12.8 13.9 L15.4 13.9 L15.1 21.8 L16.7 21.8 L17.0 16.1 L17.8 16.1 L18.1 21.8 L19.7 21.8 L19.6 12.9 C20.1 12.1 20.4 11.3 20.4 10.5 L22.1 8.3 L20.2 9.1 C18.1 8.2 14.7 8.0 11.4 8.3 C9.7 8.4 8.6 8.7 7.9 8.5 C7.3 7.9 7.0 7.1 7.0 6.3 C6.4 5.2 5.7 4.6 4.9 4.6 C4.0 4.8 3.2 5.6 2.6 6.6 Z M5.4 4.2 C6.2 2.4 7.6 1.4 9.3 1.6 C10.6 1.8 11.4 2.8 11.3 4.1 C11.2 4.5 10.9 4.5 10.8 4.1 C10.6 3.1 9.9 2.6 9.0 2.6 C7.9 2.6 7.0 3.3 6.5 4.6 Z M7.4 5.0 C8.0 4.4 8.9 4.5 9.6 5.1 C8.9 5.6 8.0 5.7 7.4 5.0 Z";

export function GoatGlyph({ size = 24, ...rest }: SVGProps<SVGSVGElement> & { size?: number | string }) {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" {...rest}>
      <path d={GOAT_PATH} />
    </svg>
  );
}
