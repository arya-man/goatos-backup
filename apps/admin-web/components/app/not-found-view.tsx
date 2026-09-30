'use client';

// The template NotFoundView (src/sections/error/not-found-view.tsx) inside the admin shell: the
// compact centred column (template SimpleCompactContent: 448px, centred text, 80px top / bottom
// from md), the bouncing title + body, PageNotFoundIllustration and the large "Go to home" button.
// Mesha keeps the shell around it (an unknown path is usually a mistyped admin URL; the sidebar is
// how the reader recovers) and the backend-owned copy. guard: not-found-template-view
import { m } from 'framer-motion';

import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';

import Link from '@/components/no-prefetch-link';
import { varBounce, MotionContainer } from '@/layouts/template/animate';
import PageNotFoundIllustration from '@/components/app/illustrations/page-not-found-illustration';

export function NotFoundView({ title, body, homeLabel }: { title: string; body: string; homeLabel: string }) {
  return (
    <Box
      data-not-found-view=""
      sx={(theme) => ({
        width: 1,
        mx: 'auto',
        display: 'flex',
        flex: '1 1 auto',
        textAlign: 'center',
        flexDirection: 'column',
        alignItems: 'center',
        p: theme.spacing(3, 2, 10, 2),
        maxWidth: 448,
        [theme.breakpoints.up('md')]: { justifyContent: 'center', p: theme.spacing(10, 0, 10, 0) },
      })}
    >
      <MotionContainer>
        <m.div variants={varBounce('in')}>
          <Typography variant="h3" component="h1" sx={{ mb: 2 }}>
            {title}
          </Typography>
        </m.div>

        <m.div variants={varBounce('in')}>
          <Typography sx={{ color: 'text.secondary' }}>{body}</Typography>
        </m.div>

        <m.div variants={varBounce('in')}>
          <PageNotFoundIllustration sx={{ my: { xs: 5, sm: 10 } }} />
        </m.div>

        <Button component={Link} href="/" size="large" variant="contained">
          {homeLabel}
        </Button>
      </MotionContainer>
    </Box>
  );
}
