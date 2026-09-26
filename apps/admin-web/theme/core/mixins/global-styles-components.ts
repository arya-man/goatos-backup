import type { Theme, CSSObject } from '@mui/material/styles';
import type { PaletteColorKey, CommonColorsKeys } from '../palette';

import { varAlpha, noRtlFlip } from 'minimal-shared/utils';

import { dividerClasses } from '@mui/material/Divider';
import { checkboxClasses } from '@mui/material/Checkbox';
import { menuItemClasses } from '@mui/material/MenuItem';
import { autocompleteClasses } from '@mui/material/Autocomplete';

// ----------------------------------------------------------------------

/**
 * Generates styles for menu item components.
 *
 * @param theme - The MUI theme object.
 * @returns A CSS object with styles.
 *
 * @example
 * ...theme.mixins.menuItemStyles(theme)
 */

export function menuItemStyles(theme: Theme): CSSObject {
  return {
    ...theme.typography.body2,
    padding: theme.spacing(0.75, 1),
    borderRadius: Number(theme.shape.borderRadius) * 0.75,
    '&:not(:last-of-type)': {
      marginBottom: 4,
    },
    [`&.${menuItemClasses.selected}`]: {
      fontWeight: theme.typography.fontWeightSemiBold,
      backgroundColor: theme.vars.palette.action.selected,
      '&:hover': { backgroundColor: theme.vars.palette.action.hover },
    },
    [`& .${checkboxClasses.root}`]: {
      padding: theme.spacing(0.5),
      marginLeft: theme.spacing(-0.5),
      marginRight: theme.spacing(0.5),
    },
    [`&.${autocompleteClasses.option}[aria-selected="true"]`]: {
      backgroundColor: theme.vars.palette.action.selected,
      '&:hover': { backgroundColor: theme.vars.palette.action.hover },
    },
    [`&+.${dividerClasses.root}`]: {
      margin: theme.spacing(0.5, 0),
    },
  };
}

// ----------------------------------------------------------------------

/**
 * Generates styles for paper components.
 *
 * @param theme - The MUI theme object.
 * @param options.blur - (Optional) Blur intensity in pixels. Defaults to 20.
 * @param options.color - (Optional) Background color. Defaults to semi-transparent paper color.
 * @param options.dropdown - (Optional) If true, applies padding, box-shadow, and border-radius for dropdowns.
 * @returns A CSS object with styles.
 *
 * @example
 * // Paper with default styles
 * ...theme.mixins.paperStyles(theme);
 *
 * @example
 * // Paper with dropdown styles and custom blur
 * ...theme.mixins.paperStyles(theme, {
 *   blur: 10,
 *   color: varAlpha(theme.vars.palette.background.defaultChannel, 0.9),
 *   dropdown: true
 * })
 */

export type PaperStyleOptions = {
  blur?: number;
  color?: string;
  dropdown?: boolean;
};

/**
 * The template's two corner glows (radial gradient, 10% opacity, top right + bottom left),
 * painted in Mesha colours instead of Minimal cyan/red: info for the top-right glow, error for the
 * bottom-left one, from the locked palette of the active scheme (theme-config -> app/mesha-theme.css).
 */
const cornerShape = (color: string, transform: string) =>
  `data:image/svg+xml,${encodeURIComponent(
    `<svg width="120" height="120" viewBox="0 0 120 120" fill="none" xmlns="http://www.w3.org/2000/svg"><rect width="120" height="120" fill="url(#g)" fill-opacity="0.1"/><defs><radialGradient id="g" cx="0" cy="0" r="1" gradientUnits="userSpaceOnUse" gradientTransform="${transform}"><stop stop-color="${color}"/><stop offset="1" stop-color="${color}" stop-opacity="0"/></radialGradient></defs></svg>`
  )}`;

function cornerShapes(theme: Theme, scheme: 'light' | 'dark'): CSSObject {
  const palette = theme.colorSchemes?.[scheme]?.palette ?? theme.palette;
  return theme.mixins.bgGradient({
    images: [
      `url("${cornerShape(palette.info.main, 'translate(120 0) rotate(-45) scale(123.25)')}")`,
      `url("${cornerShape(palette.error.main, 'translate(0 120) rotate(135) scale(123.25)')}")`,
    ],
    sizes: ['50%', '50%'],
    positions: [noRtlFlip('top right'), noRtlFlip('left bottom')],
  });
}

export function paperStyles(theme: Theme, options?: PaperStyleOptions): CSSObject {
  const { blur = 20, color, dropdown } = options ?? {};

  return {
    ...cornerShapes(theme, 'light'),
    // Not theme.applyStyles('dark'): callers (drawer, popover) spread their own applyStyles('dark')
    // after this mixin, and the shared object key would drop these dark glows.
    [theme.getColorSchemeSelector?.('dark') ?? '[data-theme="dark"] &']: cornerShapes(theme, 'dark'),
    backdropFilter: `blur(${blur}px)`,
    WebkitBackdropFilter: `blur(${blur}px)`,
    backgroundColor: color ?? varAlpha(theme.vars.palette.background.paperChannel, 0.9),
    ...(dropdown && {
      padding: theme.spacing(0.5),
      boxShadow: theme.vars.customShadows.dropdown,
      borderRadius: `${Number(theme.shape.borderRadius) * 1.25}px`,
    }),
  };
}

// ----------------------------------------------------------------------

/**
 * Generate style variant for components like Button, Chip, Label, etc.
 *
 * @param theme - The MUI theme object.
 * @param colorKey - 'default', 'inherit', or a palette color key like 'primary', 'secondary', etc.
 * @param options.hover - (Optional) Enable hover styles or provide custom hover styles.
 * @returns A CSS object with styles.
 *
 * @example
 * // Filled styles
 * ...theme.mixins.filledStyles(theme, 'inherit', { hover: true })
 * ...theme.mixins.filledStyles(theme, 'inherit', { hover: { boxShadow: theme.vars.customShadows.z8 }, })
 *
 * // Soft styles
 * ...theme.mixins.softStyles(theme, 'inherit')
 * ...theme.mixins.softStyles(theme, 'primary', { hover: true })
 */

export type ColorKey = CommonColorsKeys | PaletteColorKey | 'default' | 'inherit';

export type StyleOptions = {
  hover?: boolean | CSSObject;
};

function getHoverStyles(hoverOption: StyleOptions['hover'], hoverBase: CSSObject): CSSObject {
  if (!hoverOption) return {};

  return {
    '&:hover': {
      ...hoverBase,
      ...(typeof hoverOption === 'object' ? hoverOption : {}),
    },
  };
}

export function filledStyles(theme: Theme, colorKey: ColorKey, options?: StyleOptions): CSSObject {
  if (!colorKey) {
    console.warn(
      '[filledStyles] Missing colorKey. Please provide a valid color such as "primary", "black", or "default".'
    );
    return {};
  }

  if (colorKey === 'default') {
    const base: CSSObject = {
      color: theme.vars.palette.grey[800],
      backgroundColor: theme.vars.palette.grey[300],
    };

    const hover: CSSObject = getHoverStyles(options?.hover, {
      backgroundColor: theme.vars.palette.grey[400],
    });

    return { ...base, ...hover };
  }

  if (colorKey === 'inherit') {
    const base: CSSObject = {
      color: theme.vars.palette.common.white,
      backgroundColor: theme.vars.palette.grey[800],
      ...theme.applyStyles('dark', {
        color: theme.vars.palette.grey[800],
        backgroundColor: theme.vars.palette.common.white,
      }),
    };

    const hover: CSSObject = getHoverStyles(options?.hover, {
      backgroundColor: theme.vars.palette.grey[700],
      ...theme.applyStyles('dark', {
        backgroundColor: theme.vars.palette.grey[400],
      }),
    });

    return { ...base, ...hover };
  }

  if (colorKey === 'white' || colorKey === 'black') {
    const base: CSSObject = {
      color: `${theme.vars.palette.common[colorKey === 'white' ? 'black' : 'white']}`,
      backgroundColor: theme.vars.palette.common[colorKey],
    };

    const hover: CSSObject = getHoverStyles(options?.hover, {
      backgroundColor: varAlpha(
        `${theme.vars.palette.common[`${colorKey}Channel`]}`,
        theme.vars.opacity.filled.commonHoverBg
      ),
    });

    return { ...base, ...hover };
  }

  const colorPalette: Record<'base' | 'hover', CSSObject> = {
    base: {
      color: theme.vars.palette[colorKey].contrastText,
      backgroundColor: theme.vars.palette[colorKey].main,
    },
    hover: getHoverStyles(options?.hover, {
      backgroundColor: theme.vars.palette[colorKey].dark,
    }),
  };

  return { ...colorPalette.base, ...colorPalette.hover };
}

export function softStyles(theme: Theme, colorKey: ColorKey, options?: StyleOptions): CSSObject {
  if (!colorKey) {
    console.warn(
      '[softStyles] Missing colorKey. Please provide a valid color such as "primary", "black", or "default".'
    );
    return {};
  }

  if (colorKey === 'default') {
    return {
      ...filledStyles(theme, 'default', options),
      boxShadow: 'none',
    };
  }

  if (colorKey === 'inherit') {
    const base: CSSObject = {
      boxShadow: 'none',
      backgroundColor: varAlpha(theme.vars.palette.grey['500Channel'], theme.vars.opacity.soft.bg),
    };

    const hover: CSSObject = getHoverStyles(options?.hover, {
      backgroundColor: varAlpha(
        theme.vars.palette.grey['500Channel'],
        theme.vars.opacity.soft.hoverBg
      ),
    });

    return { ...base, ...hover };
  }

  if (colorKey === 'white' || colorKey === 'black') {
    const base: CSSObject = {
      boxShadow: 'none',
      color: theme.vars.palette.common[colorKey],
      backgroundColor: varAlpha('currentColor', theme.vars.opacity.soft.commonBg),
    };

    const hover: CSSObject = getHoverStyles(options?.hover, {
      backgroundColor: varAlpha('currentColor', theme.vars.opacity.soft.commonHoverBg),
    });

    return { ...base, ...hover };
  }

  const colorPalette: Record<'base' | 'hover', CSSObject> = {
    base: {
      boxShadow: 'none',
      color: theme.vars.palette[colorKey].dark,
      backgroundColor: varAlpha(
        theme.vars.palette[colorKey].mainChannel,
        theme.vars.opacity.soft.bg
      ),
      ...theme.applyStyles('dark', {
        color: theme.vars.palette[colorKey].light,
      }),
    },
    hover: getHoverStyles(options?.hover, {
      backgroundColor: varAlpha(
        theme.vars.palette[colorKey].mainChannel,
        theme.vars.opacity.soft.hoverBg
      ),
    }),
  };

  return { ...colorPalette.base, ...colorPalette.hover };
}
