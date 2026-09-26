// Mount the shipped bell AND lazy panel in Chromium. Only the GET feed client, the mark-read
// Server Action and Next navigation/link adapters are controlled; no live backend is contacted.
import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile, rm } from "node:fs/promises";
import { realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import test from "node:test";

const require = createRequire(import.meta.url);
const { webpack } = require("next/dist/compiled/webpack/webpack");
const { chromium } = require("playwright");
const app = fileURLToPath(new URL("../../", import.meta.url));
const repo = path.resolve(app, "../..");

async function bundle(dir) {
  const navigation = path.join(dir, "navigation.js");
  const actions = path.join(dir, "actions.js");
  const link = path.join(dir, "link.js");
  await writeFile(navigation, `import {useSyncExternalStore} from 'react';
    export function usePathname(){return useSyncExternalStore(window.bell.subscribe,()=>window.bell.pathname);}
    export function useServerInsertedHTML(){}`);
  const feedClient = path.join(dir, "feed-client.js");
  await writeFile(feedClient, `export const fetchNotificationFeed=()=>window.bell.read();
    export const fetchNotificationBadge=async()=>{window.bell.badges++;return {ok:true,unreadCount:1}};`);
  await writeFile(path.join(dir, "empty.js"), "export {};");
  await writeFile(actions, `export const markNotificationsReadAction=async(ids)=>{window.bell.marks+=ids.length;return {ok:true,readCount:ids.length}};`);
  await writeFile(link, `import React from 'react';export default React.forwardRef(function Link({prefetch,...props},ref){return React.createElement('a',{...props,ref})});`);
  await writeFile(path.join(dir, "css-loader.cjs"), "module.exports=function(){return 'export default new Proxy({},{get:(_,k)=>typeof k===\\'string\\'?k:undefined});'};");
  await writeFile(path.join(dir, "loader.cjs"), `const ts=require(${JSON.stringify(require.resolve("typescript"))});
    module.exports=function(source){return ts.transpileModule(source,{compilerOptions:{jsx:ts.JsxEmit.ReactJSX,module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2020},fileName:this.resourcePath}).outputText};`);
  await writeFile(path.join(dir, "entry.js"), `import React from 'react';import {createRoot} from 'react-dom/client';
    import {NotificationBell} from ${JSON.stringify(path.join(app, "features/notifications/notification-bell.tsx"))};
    import {AppThemeStack} from ${JSON.stringify(path.join(app, "theme/app-theme-provider.tsx"))};
    const listeners=new Set();
    const feed={available:true,unread_count:1,items:[{notification_request_id:'11111111-2222-4333-8444-000000000001',notification_type:'reminder',title:'Feed delivery ready',body:'Local fixture',status:'sent',requested_at:'2026-09-23T07:00:00Z',context:{}}]};
    window.bell={pathname:'/',calls:0,marks:0,badges:0,clock:0,hold:true,mode:'ok',
      subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},
      navigate(route){this.pathname=route;listeners.forEach(fn=>fn())},
      read(){this.calls++;if(this.hold)return new Promise(resolve=>{this.resolve=()=>resolve({ok:true,feed})});
        if(this.mode==='throw')return Promise.reject(new Error('controlled transport failure'));
        return Promise.resolve(this.mode==='error'?{ok:false,code:'test_unavailable'}:{ok:true,feed})}
    };Date.now=()=>window.bell.clock;
    createRoot(document.getElementById('root')).render(React.createElement(AppThemeStack,null,React.createElement(NotificationBell,{openLabel:'Notifications'})));`);
  const compiler = webpack({mode:"development",devtool:false,context:app,entry:path.join(dir,"entry.js"),
    output:{path:dir,filename:"bundle.js",publicPath:"/"},
    optimization:{splitChunks:false},plugins:[new webpack.optimize.LimitChunkCountPlugin({maxChunks:1}),new webpack.NormalModuleReplacementPlugin(/^node:/,(resource)=>{resource.request=resource.request.replace(/^node:/,"");})],
    resolve:{extensions:[".tsx",".ts",".js"],modules:[path.join(app,"node_modules"),path.join(repo,"node_modules"),"node_modules"],alias:{
      "@":app,"node:crypto":false,crypto:false,"server-only":false,"next/navigation$":navigation,"next/link$":link,"next/dist/client/link$":link,"./notification-actions$":actions,"./notification-feed-client$":feedClient}},
    module:{rules:[{include:realpathSync(dir),type:"javascript/esm"},{test:/\.tsx?$/,exclude:/node_modules/,use:path.join(dir,"loader.cjs")},{test:/\.css$/,loader:path.join(dir,"css-loader.cjs")}]}});
  try {
    await new Promise((resolve,reject)=>compiler.run((err,stats)=>err?reject(err):stats.hasErrors()?reject(new Error(stats.toString({all:false,errors:true}))):resolve()));
    return readFile(path.join(dir,"bundle.js"),"utf8");
  } finally { await new Promise(resolve=>compiler.close(resolve)); }
}

// The redesigned drawer has no Refresh control: opening it IS the refresh.
async function reopen(page) {
  // MUI's focus trap pulls focus back into the drawer (within 50ms) when the focused control
  // unmounts, e.g. a row's "Mark as read" button disappearing once the row is read. Escape is
  // handled by the drawer, so it is pressed once focus is inside it again.
  await page.waitForFunction(()=>document.querySelector('[role="dialog"]')?.contains(document.activeElement));
  await page.keyboard.press("Escape");
  await page.getByRole("dialog").waitFor({state:"hidden"});
  await page.getByRole("button",{name:"Notifications",exact:true}).click();
  await page.getByRole("dialog").waitFor({state:"visible"});
}

async function navigate(page, route) {
  await page.evaluate(async (route) => {
    window.bell.navigate(route);
    await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
  }, route);
}

const forbidden = /backend_down|Admin-web contract unavailable|The board could not be loaded|Weights could not be loaded/;

test("real bell reads once on mount, never on route changes, re-reads on every open, survives errors and preserves mark-read", {timeout:60_000}, async () => {
  const dir = await mkdtemp(path.join(tmpdir(),"goatos-notification-browser-"));
  let browser;
  try {
    const script = await bundle(dir);
    browser = await chromium.launch({headless:true});
    // One browser, one page, one viewport at a time. No external request is allowed.
    const page = await browser.newPage({viewport:{width:390,height:844}});
    await page.route("**/*",route=>route.abort());
    const errors=[];
    page.on("pageerror",error=>errors.push(error.message));
    const css=(await Promise.all(["app/mesha-theme.css","app/minimal-theme.css","app/frame.css","features/notifications/notification-panel.css"].map(file=>readFile(path.join(app,file),"utf8")))).join("\n");
    await page.setContent(`<style>${css}</style><div class="top"><div id="root"></div></div>`);
    // The theme stack's settings provider persists to a cookie; about:blank has no cookie jar.
    await page.evaluate(()=>Object.defineProperty(Document.prototype,"cookie",{configurable:true,get:()=>"",set:()=>{}}));
    await page.addScriptTag({content:script});
    await page.waitForFunction(()=>window.bell?.calls===1);
    // Route changes never re-read the feed (the badge poll keeps the count current).
    for(let i=0;i<20;i++) await navigate(page,`/route-${i}`);
    assert.equal(await page.evaluate(()=>window.bell.calls),1,"navigation does not read the feed");
    await page.evaluate(()=>{window.bell.hold=false;window.bell.resolve()});
    await page.getByRole("button",{name:"Notifications",exact:true}).click();
    await page.getByRole("dialog").waitFor({state:"visible"});
    await page.waitForFunction(()=>window.bell.calls===2);
    await page.getByText("Feed delivery ready",{exact:true}).waitFor({state:"visible"});
    // The sheet slides in; measure once it has settled inside the viewport.
    const fits=(width)=>page.waitForFunction((w)=>{const r=document.querySelector('[role="dialog"]')?.getBoundingClientRect();return Boolean(r&&r.width>0&&r.left>=0&&r.right<=w);},width,{timeout:5_000}).then(()=>true,()=>false);
    if(!(await fits(390))) console.log("dialog box", await page.getByRole("dialog").boundingBox());
    assert.ok(await fits(390),"first-open panel fits phone");
    await reopen(page);
    await page.waitForFunction(()=>window.bell.calls===3);
    for(let i=0;i<20;i++) await navigate(page,`/fresh-${i}`);
    assert.equal(await page.evaluate(()=>window.bell.calls),3,"navigation does not reload feed");
    await page.getByRole("button",{name:"Mark as read",exact:true}).first().click();
    await page.waitForFunction(()=>window.bell.marks===1);
    await page.evaluate(()=>{window.bell.mode='error'});
    await reopen(page);
    await page.waitForFunction(()=>window.bell.calls===4);
    for(let i=0;i<5;i++) await navigate(page,`/failure-${i}`);
    assert.equal(await page.evaluate(()=>window.bell.calls),4,"failed reads do not trigger a navigation storm");
    await page.evaluate(()=>{window.bell.mode='ok'});
    await reopen(page);
    await page.waitForFunction(()=>window.bell.calls===5);
    await page.getByText("Feed delivery ready",{exact:true}).waitFor({state:"visible"});
    assert.doesNotMatch(await page.locator("body").innerText(),forbidden);
    assert.deepEqual(errors,[]);
    await page.evaluate(()=>{window.bell.mode='throw'});
    await reopen(page);
    await page.waitForFunction(()=>window.bell.calls===6);
    await navigate(page,"/transport-failure");
    assert.equal(await page.evaluate(()=>window.bell.calls),6);
    await page.getByText("Feed delivery ready",{exact:true}).waitFor({state:"visible"});
    await page.evaluate(()=>{window.bell.mode='ok'});
    // Same mounted component, desktop size: no remount request, no clipped dialog.
    await page.setViewportSize({width:1440,height:900});
    await reopen(page);
    await page.getByText("Feed delivery ready",{exact:true}).waitFor({state:"visible"});
    await page.waitForFunction(()=>window.bell.calls===7);
    assert.ok(await fits(1440),"desktop panel fits");
    assert.doesNotMatch(await page.locator("body").innerText(),forbidden);
    assert.deepEqual(errors,[]);
  } finally { if(browser) await browser.close(); await rm(dir,{recursive:true,force:true}); }
});
