import type { Theme, Components } from '@mui/material/styles';
import type { SettingsState } from '@/layouts/template/settings';

import { cardClasses } from '@mui/material/Card';

import { appCssBaseline } from '../core/components/css-baseline';

// ----------------------------------------------------------------------

export function applySettingsToComponents(settingsState?: SettingsState): {
  components: Components<Theme>;
} {
  const MuiCssBaseline: Components<Theme>['MuiCssBaseline'] = {
    // MUI's deep merge REPLACES the core MuiCssBaseline styleOverrides function with this one, so the
    // app scrollbar (theme/core/components/css-baseline.tsx, FIXJ7) is restated here.
    styleOverrides: (theme) => ({
      ...appCssBaseline(theme),
      html: {
        fontSize: settingsState?.fontSize,
      },
      body: {
        [`& .${cardClasses.root}`]: {
          ...(settingsState?.contrast === 'high' && {
            '--card-shadow': theme.vars.customShadows.z1,
          }),
        },
      },
    }),
  };

  return {
    components: {
      MuiCssBaseline,
    },
  };
}
