/**
 * Dev-only. React's Server Components performance track (react-server-dom client, development
 * build) calls `performance.measure("​Page", { start, end })` with a NEGATIVE `end` when a
 * streaming RSC response is interrupted (a navigation lands while the previous page is still
 * streaming). Chrome throws "cannot have a negative time stamp" as an uncaught page error. It is
 * React's instrumentation, not app code; this pre-paint shim clamps the timestamps so the dev
 * console stays honest about OUR errors. It is not emitted in production builds.
 */
export const DEV_PERF_GUARD_SCRIPT = `(function(){try{var p=window.performance;if(!p||!p.measure)return;var m=p.measure.bind(p);p.measure=function(n,o,e){if(o&&typeof o==="object"){if(typeof o.start==="number"&&o.start<0)o.start=0;if(typeof o.end==="number"&&o.end<0)o.end=0;if(typeof o.start==="number"&&typeof o.end==="number"&&o.end<o.start)o.end=o.start;}return m(n,o,e)}}catch(e){}})();`;
