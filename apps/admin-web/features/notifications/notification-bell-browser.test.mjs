// Mount the shipped bell AND lazy panel in Chromium. Only the server-action transport
// and Next navigation/link adapters are controlled; no live backend is contacted.
import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile, rm } from "node:fs/promises";
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
    export function usePathname(){return useSyncExternalStore(window.bell.subscribe,()=>window.bell.pathname);}`);
  await writeFile(actions, `export const loadNotificationFeedAction=()=>window.bell.read();
    export const markNotificationsReadAction=async(ids)=>{window.bell.marks+=ids.length;return {ok:true,readCount:ids.length}};`);
  await writeFile(link, `import React from 'react';export default React.forwardRef(function Link({prefetch,...props},ref){return React.createElement('a',{...props,ref})});`);
  await writeFile(path.join(dir, "loader.cjs"), `const ts=require(${JSON.stringify(require.resolve("typescript"))});
    module.exports=function(source){return ts.transpileModule(source,{compilerOptions:{jsx:ts.JsxEmit.ReactJSX,module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2020},fileName:this.resourcePath}).outputText};`);
  await writeFile(path.join(dir, "entry.js"), `import React from 'react';import {createRoot} from 'react-dom/client';
    import {NotificationBell} from ${JSON.stringify(path.join(app, "features/notifications/notification-bell.tsx"))};
    const listeners=new Set();
    const feed={available:true,unread_count:1,items:[{notification_request_id:'11111111-2222-4333-8444-000000000001',notification_type:'reminder',title:'Feed delivery ready',body:'Local fixture',status:'sent',requested_at:'2026-09-23T07:00:00Z',context:{}}]};
    window.bell={pathname:'/',calls:0,marks:0,clock:0,hold:true,mode:'ok',
      subscribe(fn){listeners.add(fn);return()=>listeners.delete(fn)},
      navigate(route){this.pathname=route;listeners.forEach(fn=>fn())},
      read(){this.calls++;if(this.hold)return new Promise(resolve=>{this.resolve=()=>resolve({ok:true,feed})});
        if(this.mode==='throw')return Promise.reject(new Error('controlled transport failure'));
        return Promise.resolve(this.mode==='error'?{ok:false,code:'test_unavailable'}:{ok:true,feed})}
    };Date.now=()=>window.bell.clock;
    createRoot(document.getElementById('root')).render(React.createElement(NotificationBell,{openLabel:'Notifications'}));`);
  const compiler = webpack({mode:"development",devtool:false,context:app,entry:path.join(dir,"entry.js"),
    output:{path:dir,filename:"bundle.js",publicPath:"/"},
    optimization:{splitChunks:false},plugins:[new webpack.optimize.LimitChunkCountPlugin({maxChunks:1})],
    resolve:{extensions:[".tsx",".ts",".js"],modules:[path.join(app,"node_modules"),path.join(repo,"node_modules"),"node_modules"],alias:{
      "@":app,"next/navigation$":navigation,"next/link$":link,"next/dist/client/link$":link,"./notification-actions$":actions}},
    module:{rules:[{test:/\.tsx?$/,exclude:/node_modules/,use:path.join(dir,"loader.cjs")}]}});
  try {
    await new Promise((resolve,reject)=>compiler.run((err,stats)=>err?reject(err):stats.hasErrors()?reject(new Error(stats.toString({all:false,errors:true}))):resolve()));
    return readFile(path.join(dir,"bundle.js"),"utf8");
  } finally { await new Promise(resolve=>compiler.close(resolve)); }
}

async function navigate(page, route) {
  await page.evaluate(async (route) => {
    window.bell.navigate(route);
    await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
  }, route);
}

const forbidden = /backend_down|Admin-web contract unavailable|The board could not be loaded|Weights could not be loaded/;

test("real bell coalesces route bursts, opens first time, retries errors and preserves mark-read", {timeout:180_000}, async () => {
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
    await page.setContent(`<style>${await readFile(path.join(app,"app/mesha-theme.css"),"utf8")}</style><div class="top"><div id="root"></div></div>`);
    await page.addScriptTag({content:script});
    await page.waitForFunction(()=>window.bell?.calls===1);
    for(let i=0;i<20;i++) await navigate(page,`/route-${i}`);
    assert.equal(await page.evaluate(()=>window.bell.calls),1);
    await page.getByRole("button",{name:"Notifications",exact:true}).click();
    await page.getByRole("dialog",{name:"Notifications",exact:true}).waitFor({state:"visible"});
    assert.equal(await page.evaluate(()=>window.bell.calls),1,"open joins pending navigation read");
    await page.evaluate(()=>{window.bell.hold=false;window.bell.resolve()});
    await page.getByText("Feed delivery ready",{exact:true}).waitFor({state:"visible"});
    const panel=await page.getByRole("dialog").boundingBox();
    assert.ok(panel.x>=0 && panel.x+panel.width<=390,"first-open panel fits phone");
    await page.getByRole("button",{name:"Refresh",exact:true}).click();
    await page.waitForFunction(()=>window.bell.calls===2);
    for(let i=0;i<20;i++) await navigate(page,`/fresh-${i}`);
    assert.equal(await page.evaluate(()=>window.bell.calls),2,"fresh navigation does not reload feed");
    await page.evaluate(()=>{window.bell.clock=60_000});
    await navigate(page,"/stale");
    await page.waitForFunction(()=>window.bell.calls===3);
    await page.getByRole("button",{name:"Mark as read",exact:true}).click();
    await page.waitForFunction(()=>window.bell.marks===1);
    await page.evaluate(()=>{window.bell.mode='error'});
    await page.getByRole("button",{name:"Refresh",exact:true}).click();
    await page.getByText("Notifications could not be loaded. Try again in a moment.",{exact:true}).waitFor();
    for(let i=0;i<5;i++) await navigate(page,`/failure-${i}`);
    assert.equal(await page.evaluate(()=>window.bell.calls),4,"failed reads do not trigger navigation storm");
    await page.evaluate(()=>{window.bell.mode='ok'});
    await page.getByRole("button",{name:"Refresh",exact:true}).click();
    await page.waitForFunction(()=>window.bell.calls===5);
    await page.getByText("Notifications could not be loaded. Try again in a moment.",{exact:true}).waitFor({state:"hidden"});
    assert.doesNotMatch(await page.locator("body").innerText(),forbidden);
    assert.deepEqual(errors,[]);
    await page.evaluate(()=>{window.bell.mode='throw'});
    await page.getByRole("button",{name:"Refresh",exact:true}).click();
    await page.waitForFunction(()=>window.bell.calls===6);
    await navigate(page,"/transport-failure");
    assert.equal(await page.evaluate(()=>window.bell.calls),6);
    await page.getByText("Feed delivery ready",{exact:true}).waitFor({state:"visible"});
    await page.evaluate(()=>{window.bell.mode='ok'});
    // Same mounted component, desktop size: no remount request, no clipped dialog.
    await page.setViewportSize({width:1440,height:900});
    await page.getByRole("button",{name:"Close notifications",exact:true}).click();
    await page.getByRole("button",{name:"Notifications",exact:true}).click();
    await page.getByText("Feed delivery ready",{exact:true}).waitFor({state:"visible"});
    await page.waitForFunction(()=>window.bell.calls===7);
    const desktop=await page.getByRole("dialog").boundingBox();
    assert.ok(desktop.x>=0 && desktop.x+desktop.width<=1440);
    assert.doesNotMatch(await page.locator("body").innerText(),forbidden);
    assert.deepEqual(errors,[]);
  } finally { if(browser) await browser.close(); await rm(dir,{recursive:true,force:true}); }
});
