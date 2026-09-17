const fs=require('fs'),vm=require('vm'),assert=require('assert/strict');const root=process.cwd();const elements=new Map();function element(s){if(!elements.has(s))elements.set(s,{options:[],value:s==='#role'?'CEO / CXO':'',innerHTML:'',textContent:'',style:{},dataset:{},classList:{contains(){return false},toggle(){},add(){},remove(){}},addEventListener(){},setAttribute(){},insertAdjacentHTML(){},querySelector(){return null},querySelectorAll(){return[]},remove(){},prepend(){},append(){},appendChild(){},getBoundingClientRect(){return{width:1200,height:800,left:0,top:0}}});return elements.get(s)}const c={MutationObserver:class{observe(){}},console,structuredClone,setTimeout:()=>0,clearTimeout(){},crypto:require('crypto').webcrypto,localStorage:{getItem(){return null},setItem(){}},document:{createElement:()=>element('created'),querySelector:element,querySelectorAll(){return[]},getElementById:s=>element('#'+s),body:element('body'),documentElement:element('html'),addEventListener(){}},window:{addEventListener(){},scrollTo(){}},location:{hash:''},requestAnimationFrame(){}};Object.assign(c,c.window);c.window=c;vm.createContext(c);const scripts=[...fs.readFileSync('index.html','utf8').matchAll(/<script src="([^"?]+)(?:\?[^\"]*)?"/g)].map(m=>m[1]);for(const f of scripts){try{if(f==='production-shell.js')vm.runInContext('render=()=>{}',c);vm.runInContext(fs.readFileSync(f,'utf8'),c,{filename:f})}catch(e){console.error('LOAD',f,e);process.exit(1)}}function run(code){return vm.runInContext(code,c)}if(!scripts.includes('sop-composition.js'))vm.runInContext(fs.readFileSync('sop-composition.js','utf8'),c);run('ensureTypedConfig();ensureSources();render=()=>{};renderItems=()=>{};closeModal=()=>{};');




run('ensureSourceSops()');
assert.equal(run("[state.sops.Procurement,...state.archivedSops.Procurement].filter(s=>s.title==='Animal Purchase Inspection').length"),1);
assert.equal(run("state.sops.Procurement.nodes.filter(n=>n.type==='question').length"),47);
run("current='Procurement';sopsView()");assert.equal((element('#content').innerHTML.match(/Animal Purchase Inspection/g)||[]).length,1);
for(const cohort of ['Adults','Kids']){
 run(`current='Health';window.courseGraph=[state.sops.Health,...state.archivedSops.Health].find(s=>s.title==='Fever · ${cohort}')`);
 assert.equal(run('window.courseGraph.nodes.filter(n=>n.type===\'action\').length'),16);
 assert.equal(run(`JSON.stringify(window.courseGraph.nodes.filter(n=>n.type==='action').map(n=>n.sourceMetadata))`),run(`JSON.stringify(HEALTH_FEVER_FIXTURES['${cohort}'])`));
 assert.equal(run('validateWorkflow(window.courseGraph,"Health").length'),0);
}
for(const mode of ['individual','lump_sum']){
 run(`window.weighGraph=weighingSeed('${mode}')`);
 assert.equal(run('validateWorkflow(window.weighGraph,"Weighing").length'),0);
 assert.equal(run("window.weighGraph.nodes.some(n=>n.label==='Weighing mode')"),false);
 assert.equal(run("window.weighGraph.nodes.find(n=>n.proof).proof.slot"),mode==='individual'?'animal_video':'weighing_lump_sum_video');
 assert.equal(run("window.weighGraph.nodes.find(n=>n.proof).proof.maxFiles"),mode==='individual'?1:5);
 assert.equal(run("window.weighGraph.nodes.find(n=>n.id==='review-branch').no"),'rework');
 assert.equal(run("JSON.stringify(window.weighGraph.sourceWorkflow)"),run("JSON.stringify(WEIGHING_SOURCE.workflow)"));
}
run("current='Feed';sopsView()");for(const title of ['Feed Distribution','Feed Packing','Feed Transport'])assert(element('#content').innerHTML.includes(title));
assert(!element('#content').innerHTML.includes('Open flow chart'));
assert(!element('#content').innerHTML.includes('Edit SOP'));
const archivedHandler=element('#content').innerHTML.match(/onclick="([^"]*switchSop[^"]*)"/)[1];
const expectedTitle=run('state.archivedSops.Feed[0].title');
run(archivedHandler);assert.equal(run('state.sops.Feed.title'),expectedTitle);assert.equal(run('tab'),'Editor');
assert.equal(run('state.archivedSops.Feed.length'),2);

run("current='Health';sopsView()");assert(!element('#content').innerHTML.includes('No fields yet'));
// A revisit must retain authored changes and cannot duplicate imported cards.
run("state.sops.Health.nodes[1].label='Edited source step';ensureSourceSops()");assert.equal(run('state.sops.Health.nodes[1].label'),'Edited source step');assert.equal(run('state.archivedSops.Health.length'),1);
console.log('PASS source SOPs: one procurement import; Health source rows; assigned-mode weighing proof identity and rework; Feed graph access; idempotent migration');

run("current='Health';ensureSourceSops()");assert.equal(run('sourcePages().reduce((sum,p)=>sum+p.count,0)'),16);assert.equal(run('sourcePages().map(p=>p.count).join(",")'),'6,5,5');

for(const kind of ['direction','packing','transport']){
 run(`window.feedGraph=feedSeed('${kind}')`);assert.equal(run('validateWorkflow(window.feedGraph,"Feed").length'),0);
 run('window.feedCompiled=compileWorkflow(window.feedGraph,"Feed",1)');
 assert.equal(run('window.feedGraph.nodes.find(n=>n.id==="verify").next'),'feed-review-result');
 assert.equal(run('window.feedGraph.nodes.find(n=>n.id==="feed-review-branch").yes'),'end');
 assert.equal(run('window.feedGraph.nodes.find(n=>n.id==="feed-review-branch").no'),'feed-rework');
 assert.equal(run('window.feedGraph.nodes.find(n=>n.id==="feed-rework").label'),run(`FEED_SOURCE['${kind}'].workflow.nodes.find(n=>n.key==='rework').label`));
 assert.equal(run('JSON.stringify(window.feedGraph.sourceWorkflow)'),run(`JSON.stringify(FEED_SOURCE['${kind}'].workflow)`));
 if(kind!=='transport'){assert.equal(run('window.feedGraph.nodes.find(n=>n.label==="Session").unitOptional'),true);assert.equal(run('window.feedGraph.nodes.find(n=>n.label==="Session").max'),null);}
}
run("window.lump=weighingSeed('lump_sum').nodes.find(n=>n.sourceQuestionId==='total_weight_kg')");assert.equal(run('window.lump.max'),null);assert(!run('simInput(window.lump)').includes('max="999"'));
// Correct persisted generated fields while retaining authored limits and graph layout.
run("state.sops.Feed=feedSeed('direction');state.sops.Feed.nodes.find(n=>n.label==='Session').unitOptional=false;state.sops.Feed.nodes.find(n=>n.label==='Session').max=999;state.sops.Feed.nodes.find(n=>n.label==='Session').x=888;state.sops.Weighing=weighingSeed('lump_sum');state.sops.Weighing.nodes.find(n=>n.sourceQuestionId==='total_weight_kg').max=12000;migrateSourceFieldCorrections()");assert.equal(run("state.sops.Feed.nodes.find(n=>n.label==='Session').max"),null);assert.equal(run("state.sops.Feed.nodes.find(n=>n.label==='Session').x"),888);assert.equal(run("state.sops.Weighing.nodes.find(n=>n.sourceQuestionId==='total_weight_kg').max"),12000);
run("var oldFeed=feedSeed('transport');oldFeed.nodes=oldFeed.nodes.filter(n=>!n.id.startsWith('feed-review')&&n.id!=='feed-rework');oldFeed.nodes.find(n=>n.id==='verify').next='end';oldFeed.nodes.find(n=>n.id==='verify').label='Verifier reviews submitted evidence';state.sops.Feed=oldFeed;migrateSourceFieldCorrections();migrateSourceFieldCorrections()");assert.equal(run("state.sops.Feed.nodes.filter(n=>n.id==='feed-rework').length"),1);assert.equal(run('validateWorkflow(state.sops.Feed,"Feed").length'),0);
console.log('PASS all Feed source graphs validate/compile, exact approval/rework paths, dimensionless sessions, unbounded weight source and idempotent saved-draft repair');

// Startup migration must repair direct editor routes without a visit to the SOP list.
run("var retained=feedSeed('transport');retained.nodes=retained.nodes.filter(n=>!n.id.startsWith('feed-review')&&n.id!=='feed-rework');retained.nodes.find(n=>n.id==='verify').next='end';retained.nodes.find(n=>n.id==='verify').label='Verifier reviews submitted evidence';Object.assign(retained.nodes.find(n=>n.id==='end'),{label:'Custom approved terminal',x:999,y:2222});state.sops.Feed=retained;location.hash='#Feed/Editor';migrateBuiltInSops()");
assert.equal(run("state.sops.Feed.nodes.find(n=>n.id==='end').label"),'Custom approved terminal');assert.equal(run("state.sops.Feed.nodes.find(n=>n.id==='end').x"),999);assert.equal(run("state.sops.Feed.nodes.find(n=>n.id==='end').y"),2222);assert.equal(run("state.sops.Feed.nodes.find(n=>n.id==='verify').next"),'feed-review-result');assert.equal(run("state.sops.Feed.nodes.filter(n=>n.id==='feed-rework').length"),1);assert.equal(run('validateWorkflow(state.sops.Feed,"Feed").length'),0);
console.log('PASS direct editor startup migration repairs Feed review edges and preserves authored terminal text and position');
