const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict');
let dialog=null,callback,listener;const bg=[{},{},{}];let active;
function element(text){return {textContent:text,id:'',tabIndex:0,isConnected:true,disabled:false,getAttribute(){return null},getClientRects(){return [1]},closest(){return null},focus(){active=this}}}
const opener=element('Start preview');active=opener;
function makeDialog(){const first=element('Close'),last=element('Run');dialog={tabIndex:0,children:[first,last],setAttribute(){},querySelector(){return {id:''}},querySelectorAll(){return this.children},contains(e){return this.children.includes(e)},focus(){active=this}};return dialog}
const doc={get activeElement(){return active},querySelector(s){if(s==='#overlay .modal')return dialog;if(s==='#overlay')return {};return bg[['header','#nav','#main'].indexOf(s)]},querySelectorAll(){return [opener]},addEventListener(t,f){listener=f}};
const ctx={document:doc,modal:makeDialog,closeModal(){dialog=null},MutationObserver:class{constructor(f){callback=f}observe(){}},console};vm.createContext(ctx);vm.runInContext(fs.readFileSync('modal-accessibility.js','utf8'),ctx);
function key(k,shift=false){let prevented=false;listener({key:k,shiftKey:shift,preventDefault(){prevented=true},stopImmediatePropagation(){}});return prevented}
// Direct preview rendering bypasses modal(): observer must capture its opener.
makeDialog();callback();assert.equal(active.textContent,'Close');assert.ok(bg.every(e=>e.inert));assert.ok(key('Tab',true));assert.equal(active.textContent,'Run');assert.ok(key('Tab'));assert.equal(active.textContent,'Close');
// Replacing a running preview must retain the original opener.
makeDialog();callback();key('Escape');assert.equal(active,opener);assert.ok(bg.every(e=>!e.inert));
ctx.modal();const replacement=dialog;callback();assert.equal(active,replacement.children[0]);ctx.closeModal();assert.equal(active,opener);
// Direct teardown cannot strand the application inert.
makeDialog();callback();dialog=null;callback();assert.ok(bg.every(e=>!e.inert));
console.log('PASS modal: direct preview opener, replacement, forward/reverse trap, Escape/close restore, inert cleanup');
