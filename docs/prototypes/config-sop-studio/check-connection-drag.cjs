const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'@playwright/test');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true});
 try{
 for(const width of [1440,760]){
 const page=await browser.newPage({viewport:{width,height:1000}});
 if(process.env.BASELINE_JS)await page.route('**/canvas-editor.js*',r=>r.fulfill({path:process.env.BASELINE_JS,contentType:'text/javascript'}));
 await page.goto('http://127.0.0.1:4318/#Procurement/Editor');
 await page.waitForSelector('.flow-node');
 const ids=await page.evaluate(()=>{
 const nodes=sop().nodes,from=nodes.find(n=>n.type==='question'),to=nodes.find(n=>n.type==='end');
 canvasView={x:20,y:20,scale:.85};from.x=50;from.y=20;to.x=50;to.y=240;selected=from.id;drawGraph();
 return {from:from.id,to:to.id,before:from.next};
 });
 const output=page.locator(`[data-output-id="${ids.from}"]`),target=page.locator(`[data-node="${ids.to}"] .flow-node-title`);
 const a=await output.boundingBox(),b=await target.boundingBox();
 await page.mouse.move(a.x+a.width/2,a.y+a.height/2);await page.mouse.down();await page.mouse.move(b.x+b.width/2,b.y+b.height/2,{steps:12});
 assert.equal(await page.locator('.connection-target').count(),1,'Destination box highlights during drag');
 await page.mouse.up();
 assert.equal(await page.evaluate(id=>sop().nodes.find(n=>n.id===id).next,ids.from),ids.to,'Box-body drop reconnects');
 await page.evaluate(()=>canvasUndo());
 assert.equal(await page.evaluate(id=>sop().nodes.find(n=>n.id===id).next,ids.from),ids.before,'Undo restores destination');
 await output.click();await target.click();
 assert.equal(await page.evaluate(id=>sop().nodes.find(n=>n.id===id).next,ids.from),ids.to,'Click handle then box reconnects');
 await page.evaluate(()=>canvasUndo());
 const a2=await output.boundingBox();await page.mouse.move(a2.x+a2.width/2,a2.y+a2.height/2);await page.mouse.down();await page.mouse.move(a2.x+310,a2.y+40,{steps:8});await page.keyboard.press('Escape');await page.mouse.up();
 assert.equal(await page.evaluate(id=>sop().nodes.find(n=>n.id===id).next,ids.from),ids.before,'Escape leaves graph unchanged');
 assert.equal(await page.locator('.connection-target').count(),0);
 // Reconnect the existing arrow by its line, endpoint, and central +.
 await page.evaluate(ids=>{const nodes=sop().nodes;for(const n of nodes){n.x=1500;n.y=1500}const from=nodes.find(n=>n.id===ids.from),old=nodes.find(n=>n.id===ids.before),to=nodes.find(n=>n.id===ids.to);from.x=30;from.y=20;old.x=30;old.y=190;to.x=30;to.y=370;drawGraph()},ids);
 for(const origin of ['line','endpoint','plus']){
 const start=await page.evaluate(({ids,origin})=>{
 let el,p;
 if(origin==='line'){el=document.querySelector(`[data-edge-id="${ids.from}"][data-edge-key="next"]`);p=el.getPointAtLength(el.getTotalLength()*.23);p=new DOMPoint(p.x,p.y).matrixTransform(el.getScreenCTM());return {x:p.x,y:p.y}}
 el=document.querySelector(origin==='endpoint'?`[data-input="${ids.before}"]`:`[data-plus-id="${ids.from}"][data-plus-key="next"]`);const r=el.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2};
 },{ids,origin});
 const dest=await target.boundingBox();
 await page.mouse.move(start.x,start.y);await page.mouse.down();await page.mouse.move(dest.x+dest.width/2,dest.y+dest.height/2,{steps:16});await page.mouse.up();
 assert.equal(await page.evaluate(id=>sop().nodes.find(n=>n.id===id).next,ids.from),ids.to,origin+' drag reconnects existing arrow');
 assert.equal(await page.locator('#overlay.open').count(),0,'Dragging plus does not open insertion dialog');
 await page.evaluate(()=>canvasUndo());
 assert.equal(await page.evaluate(id=>sop().nodes.find(n=>n.id===id).next,ids.from),ids.before,'Arrow undo restores original');
 }
 console.log(`PASS ${width}px: existing arrow line / endpoint / plus drag and undo`);
 console.log(`PASS ${width}px: real pointer drag onto box, highlight, undo, click-connect, Escape`);
 const beforeDelete=await page.evaluate(ids=>{
 const q=sop().nodes.find(n=>n.id===ids.from);const decision={id:'delete-check',type:'condition',label:'Check the answer',source:q.id,clauses:[{source:q.id,op:'>',value:'0'}],op:'>',value:'0',yes:ids.to,no:ids.to,x:350,y:190};sop().nodes.push(decision);selected=q.id;drawGraph();return JSON.stringify(sop().nodes);
 },ids);
 await page.locator(`[data-node="${ids.from}"] .flow-delete`).click();
 assert.equal(await page.locator(`[data-node="${ids.from}"]`).count(),0,'X removes referenced question');
 assert.equal(await page.evaluate(()=>sop().nodes.find(n=>n.id==='delete-check').source),'');
 assert.equal(await page.evaluate(()=>sop().nodes.find(n=>n.id==='delete-check').clauses[0].source),'');
 assert.ok(await page.evaluate(()=>validateWorkflow(sop()).some(e=>e.includes('answer source for every check'))));
 await page.evaluate(()=>canvasUndo());
 assert.equal(await page.evaluate(()=>JSON.stringify(sop().nodes)),beforeDelete,'Undo restores question and every source');
 console.log(`PASS ${width}px: delete referenced question via X, invalidate affected checks, full undo`);
 await page.close();
 }
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
