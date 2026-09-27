'use client';

// Template-derived (docs/design/template-derived.json): Minimal v7.7.0 next-ts
// src/sections/overview/app/app-welcome.tsx with ONLY its demo wiring changed: the demo background
// photo (assets/background/background-5.webp, a template image) is dropped from the gradient (Mesha
// ships no template imagery), and `title` / `description` accept nodes so the drive-day status can
// carry a Label. Markup and sx are the template's (guard: template-derived-anatomy).
import type { BoxProps } from '@mui/material/Box';

import { varAlpha } from 'minimal-shared/utils';

import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

// ----------------------------------------------------------------------

type Props = Omit<BoxProps, 'title'> & {
  title?: React.ReactNode;
  description?: React.ReactNode;
  img?: React.ReactNode;
  action?: React.ReactNode;
};

export function AppWelcome({ title, description, action, img, sx, ...other }: Props) {
  return (
    <Box
      sx={[
        (theme) => ({
          ...theme.mixins.bgGradient({
            images: [
              `linear-gradient(to right, ${varAlpha(theme.vars.palette.grey['900Channel'], 0.88)} 0%, ${theme.vars.palette.grey[900]} 75%)`,
            ],
          }),
          pt: 5,
          pb: 5,
          pr: 3,
          gap: 5,
          borderRadius: 2,
          display: 'flex',
          height: { md: 1 },
          position: 'relative',
          pl: { xs: 3, md: 5 },
          alignItems: 'center',
          color: 'common.white',
          textAlign: { xs: 'center', md: 'left' },
          flexDirection: { xs: 'column', md: 'row' },
          border: `solid 1px ${theme.vars.palette.grey[800]}`,
        }),
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
      {...other}
    >
      <Box
        sx={{
          display: 'flex',
          flex: '1 1 auto',
          flexDirection: 'column',
          alignItems: { xs: 'center', md: 'flex-start' },
        }}
      >
        <Typography variant="h4" sx={{ whiteSpace: 'pre-line', mb: 1 }}>
          {title}
        </Typography>

        <Typography variant="body2" sx={{ opacity: 0.64, maxWidth: 360, ...(action && { mb: 3 }) }}>
          {description}
        </Typography>

        {action && action}
      </Box>

      {img && <Box sx={{ maxWidth: 260 }}>{img}</Box>}
    </Box>
  );
}
