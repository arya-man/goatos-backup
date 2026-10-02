import type { AvatarGroupClassKey } from '@mui/material/AvatarGroup';
import type { Theme, Components, ComponentsVariants } from '@mui/material/styles';
import type { PaletteColorKey } from '../palette';

import { parseCssVar } from 'minimal-shared/utils';

import Box from '@mui/material/Box';

import { colorKeys } from '../palette';

// ----------------------------------------------------------------------

/**
 * TypeScript extension for MUI theme augmentation.
 * @to {@link file://./../../extend-theme-types.d.ts}
 */
export type AvatarGroupExtendVariant = { compact: true };
export type AvatarExtendColor = {
  color?: PaletteColorKey | 'default' | 'inherit';
};

type AvatarVariants = ComponentsVariants<Theme>['MuiAvatar'];
type AvatarGroupVariants = ComponentsVariants<Theme>['MuiAvatarGroup'];

const baseColors = ['default', 'inherit'] as const;
const allColors = [...baseColors, ...colorKeys.palette] as const;
/**
 * The colours a NAME may be hashed to. Not `inherit`: that is the template's ink fill (grey 800 in
 * light, white in dark), so every person whose initial landed on it rendered as a solid black square
 * on the light theme (PR #294 L5). `color="inherit"` passed explicitly still works.
 */
const nameColors = allColors.filter((color) => color !== 'inherit');

export function getAvatarColor(
  inputValue?: string,
  fallback: AvatarExtendColor['color'] = 'default'
): string {
  if (!inputValue?.trim()) {
    return fallback;
  }

  const firstChar = inputValue.trim()[0].toLowerCase();

  // Only handle alphabet characters a-z
  if (!/[a-z]/.test(firstChar)) {
    return fallback;
  }

  const alphabetIndex = firstChar.charCodeAt(0) - 'a'.charCodeAt(0); // 0 for 'a', 25 for 'z'
  const colorIndex = alphabetIndex % nameColors.length;

  return nameColors[colorIndex] || fallback;
}

const customRenderSurplus = (surplus: number) => (
  <Box
    component="span"
    sx={[
      (theme) => ({
        width: 1,
        height: 1,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        position: 'absolute',
        color: theme.vars.palette.primary.dark,
        backgroundColor: theme.vars.palette.primary.lighter,
        fontSize: {
          '@': theme.typography.pxToRem(11),
          '@32': theme.typography.pxToRem(12),
          '@36': theme.typography.pxToRem(13),
          '@40': theme.typography.pxToRem(14),
          '@64': theme.typography.pxToRem(18),
        },
      }),
    ]}
  >
    +{surplus}
  </Box>
);

/* **********************************************************************
 * 🗳️ Variants
 * **********************************************************************/
const colorVariants = [
  {
    props: {},
    style: ({ theme }) => ({
      color: theme.vars.palette.action.active,
      [parseCssVar(theme.vars.palette.Avatar.defaultBg)]: theme.vars.palette.grey[300],
      ...theme.applyStyles('dark', {
        [parseCssVar(theme.vars.palette.Avatar.defaultBg)]: theme.vars.palette.grey[700],
      }),
    }),
  },
  {
    props: (props) =>
      props.color === 'inherit' || (!!props.alt && getAvatarColor(props.alt) === 'inherit'),
    style: ({ theme }) => ({
      ...theme.mixins.filledStyles(theme, 'inherit'),
    }),
  },
  ...(colorKeys.palette.map((colorKey) => ({
    props: (props) =>
      props.color === colorKey || (!!props.alt && getAvatarColor(props.alt) === colorKey),
    style: ({ theme }) => ({
      color: theme.vars.palette[colorKey].contrastText,
      backgroundColor: theme.vars.palette[colorKey].main,
    }),
  })) satisfies AvatarVariants),
] satisfies AvatarVariants;

const avatarGroupVariants = {
  root: [
    {
      props: (props) => props.variant === 'compact',
      style: { width: 40, height: 40, position: 'relative' },
    },
  ],
  avatar: [
    {
      props: (props) => props.variant === 'compact',
      style: {
        margin: 0,
        width: 28,
        height: 28,
        position: 'absolute',
        '&:first-of-type': { left: 0, bottom: 0, zIndex: 9 },
        '&:last-of-type': { top: 0, right: 0 },
      },
    },
  ],
} satisfies Record<AvatarGroupClassKey, AvatarGroupVariants>;

/* **********************************************************************
 * 🧩 Components
 * **********************************************************************/
const MuiAvatar: Components<Theme>['MuiAvatar'] = {
  // ▼▼▼▼▼▼▼▼ 🎨 STYLE ▼▼▼▼▼▼▼▼
  styleOverrides: {
    root: ({ theme }) => ({
      containerType: 'inline-size',
      fontSize: theme.typography.pxToRem(18),
      fontWeight: theme.typography.fontWeightMedium,
    }),
    colorDefault: {
      variants: [...colorVariants],
    },
    rounded: ({ theme }) => ({
      borderRadius: Number(theme.shape.borderRadius) * 1.5,
    }),
  },
};

const MuiAvatarGroup: Components<Theme>['MuiAvatarGroup'] = {
  // ▼▼▼▼▼▼▼▼ ⚙️ PROPS ▼▼▼▼▼▼▼▼
  defaultProps: {
    max: 4,
    renderSurplus: (surplus: number) => customRenderSurplus(surplus),
  },
  // ▼▼▼▼▼▼▼▼ 🎨 STYLE ▼▼▼▼▼▼▼▼
  styleOverrides: {
    root: {
      justifyContent: 'flex-end',
      variants: [...avatarGroupVariants.root],
    },
    avatar: {
      variants: [...avatarGroupVariants.avatar],
    },
  },
};

/* **********************************************************************
 * 🚀 Export
 * **********************************************************************/
export const avatar: Components<Theme> = {
  MuiAvatar,
  MuiAvatarGroup,
};
