import type { ReactNode } from "react";
import type { AlertProps } from "@mui/material/Alert";

import Alert from "@mui/material/Alert";
import Stack from "@mui/material/Stack";

/**
 * An MUI Alert carrying MORE THAN ONE action button. MUI puts `action` in a no-wrap column beside
 * the message, so at 390 two buttons were squeezed into ~50px and broke one word per line
 * ("Open / the / draft", J3 P1-3). Here the actions sit beside the message from `sm` up and drop
 * under it, right-aligned and full width, on phones; button labels never wrap.
 * guard: alert-actions-stack (components/app/action-alert.test.mjs)
 */
export function ActionAlert({ actions, sx, children, ...other }: Omit<AlertProps, "action"> & { actions: ReactNode }) {
  return (
    <Alert
      {...other}
      action={
        <Stack direction="row" spacing={1} sx={{ flexWrap: "wrap", justifyContent: "flex-end", rowGap: 1, "& .MuiButton-root": { whiteSpace: "nowrap" } }}>
          {actions}
        </Stack>
      }
      sx={[
        {
          flexWrap: { xs: "wrap", sm: "nowrap" },
          "& .MuiAlert-message": { flex: { xs: "1 1 0", sm: "0 1 auto" }, minWidth: 0 },
          "& .MuiAlert-action": { ml: { xs: 0, sm: "auto" }, pl: { xs: 0, sm: 2 }, pt: { xs: 0.5, sm: "4px" }, width: { xs: 1, sm: "auto" }, justifyContent: "flex-end" },
        },
        ...(Array.isArray(sx) ? sx : sx ? [sx] : []),
      ]}
    >
      {children}
    </Alert>
  );
}
