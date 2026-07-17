import NextLink, { type LinkProps as NextLinkProps } from "next/link";
import { forwardRef, type AnchorHTMLAttributes } from "react";

type NoPrefetchLinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, keyof NextLinkProps | "href"> &
  Omit<NextLinkProps, "prefetch"> & {
    prefetch?: false | null;
  };

const Link = forwardRef<HTMLAnchorElement, NoPrefetchLinkProps>(function NoPrefetchLink(
  { prefetch = false, ...props },
  ref,
) {
  return <NextLink ref={ref} prefetch={prefetch} {...props} />;
});

export default Link;
