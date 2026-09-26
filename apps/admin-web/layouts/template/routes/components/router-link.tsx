// Template RouterLink is next/link. Admin-web links never prefetch (perf budget), so the shared
// no-prefetch wrapper is used under the same export name.
import RouterLink from '@/components/no-prefetch-link';

export { RouterLink };
