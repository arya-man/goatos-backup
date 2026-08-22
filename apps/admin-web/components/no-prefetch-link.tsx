import NextLink from "next/link";
import { forwardRef, type ComponentPropsWithoutRef } from "react";

// Props are taken from NextLink ITSELF rather than rebuilt from AnchorHTMLAttributes + LinkProps.
//
// The hand-built version composed React's own AnchorHTMLAttributes with next/link's LinkProps, and
// those two arrive from DIFFERENT copies of @types/react in this workspace (18.3.31 and 19.2.17
// are both installed). Two ReactNode types that are structurally identical but nominally distinct
// do not unify, so `children` was reported as incompatible with itself and `next build` failed --
// while `next dev` compiled happily, because dev does not typecheck.
//
// Deriving from NextLink keeps every prop it accepts, stays correct if next/link changes, and
// never puts two @types/react copies on opposite sides of an assignment.
type NoPrefetchLinkProps = Omit<ComponentPropsWithoutRef<typeof NextLink>, "prefetch"> & {
  prefetch?: false | null;
};

const Link = forwardRef<HTMLAnchorElement, NoPrefetchLinkProps>(function NoPrefetchLink(
  { prefetch = false, ...props },
  ref,
) {
  return <NextLink ref={ref} prefetch={prefetch} {...props} />;
});

export default Link;
