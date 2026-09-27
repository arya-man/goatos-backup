/** A leading filter control in the template order toolbar (components/app/sections/order): full
 * width on a phone (the toolbar stacks); from md up to the template's field width (160; its
 * DatePickers are maxWidth 200) and shrinkable, so the search keeps its minimum. */
export const orderToolbarFilterSx = { width: { xs: 1, md: 'auto' }, flex: { md: '0 1 160px' }, minWidth: 0 } as const;

/** The toolbar's search slot element grows like the template's fullWidth search TextField and never
 * drops under 240px from md (the filters shrink first). */
export const orderToolbarSearchSx = { flex: '1 1 240px', minWidth: { xs: 0, md: 240 } } as const;
